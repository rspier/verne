package server

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"nntp-web/internal/mimeparser"
	"nntp-web/internal/models"
	"nntp-web/internal/utils"
	"github.com/gatherstars-com/jwz"
)

type DisplayThreadItem struct {
	Message                   jwz.Threadable
	Depth                     int
	Children                  []*DisplayThreadItem
	MessagesInSubThreadCount int
}

func buildDisplayTree(rootContainer jwz.Threadable, currentDepth int) ([]*DisplayThreadItem, int) {
	var items []*DisplayThreadItem
	var totalMessagesInSubTree int = 0
	sibling := rootContainer
	for sibling != nil {
		if !sibling.IsDummy() {
			item := &DisplayThreadItem{
				Message: sibling,
				Depth:   currentDepth,
			}
			messagesInThisBranch := 1
			if child := sibling.GetChild(); child != nil {
				var childrenItems []*DisplayThreadItem
				var countFromChildren int
				childrenItems, countFromChildren = buildDisplayTree(child, currentDepth+1)
				item.Children = childrenItems
				messagesInThisBranch += countFromChildren
			}
			item.MessagesInSubThreadCount = messagesInThisBranch
			items = append(items, item)
			totalMessagesInSubTree += messagesInThisBranch
		} else {
			log.Printf("Skipping dummy item: %s, processing its children at current depth %d.", sibling.MessageThreadID(), currentDepth)
			if child := sibling.GetChild(); child != nil {
				childrenOfDummy, countFromDummyChildren := buildDisplayTree(child, currentDepth)
				items = append(items, childrenOfDummy...)
				totalMessagesInSubTree += countFromDummyChildren
			}
		}
		sibling = sibling.GetNext()
	}
	return items, totalMessagesInSubTree
}

func (s *Server) handleListGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		allGroups, err := s.db.GetAllNewsgroups(ctx)
		if err != nil {
			log.Printf("Error fetching newsgroups: %v", err)
			http.Error(w, "Failed to retrieve newsgroups", http.StatusInternalServerError)
			return
		}
		var activeGroups, slowGroups, inactiveGroups []models.Newsgroup
		now := time.Now()
		oneMonthAgo := now.AddDate(0, -1, 0)
		oneYearAgo := now.AddDate(-1, 0, 0)
		const activityLookbackDays = 30
		for _, group := range allGroups {
			group.ShowAvgPosts = false
			if group.LastPostDate != nil {
				if group.LastPostDate.After(oneMonthAgo) {
					avgPosts, _, hasActivity, errStats := s.db.GetGroupActivityStats(ctx, group.ID, activityLookbackDays)
					if errStats != nil {
						log.Printf("Error fetching activity stats for group %s (ID %d): %v", group.Name, group.ID, errStats)
					} else if hasActivity {
						group.AvgPostsLastMonth = avgPosts
						group.ShowAvgPosts = true
					}
					activeGroups = append(activeGroups, group)
				} else if group.LastPostDate.After(oneYearAgo) {
					slowGroups = append(slowGroups, group)
				} else {
					inactiveGroups = append(inactiveGroups, group)
				}
			} else {
				inactiveGroups = append(inactiveGroups, group)
			}
		}
		data := map[string]interface{}{
			"ActiveGroups":   activeGroups,
			"SlowGroups":     slowGroups,
			"InactiveGroups": inactiveGroups,
			"Error":          nil,
		}
		s.renderTemplate(w, r, "groups.html.tmpl", data)
	}
}

const robotsTXTContent = `User-agent: *
Disallow: /group/perl.cpan.testers/
User-agent: *
Disallow: /group/perl.daily-build.reports/
User-agent: Googlebot
Allow: /
User-agent: Applebot
Allow: /
User-agent: *
Disallow: /
User-agent: Yandex
Disallow: /
User-agent: Bytespider
Disallow: /
User-agent: DataForSeoBot
Disallow: /
User-agent: AhrefsBot
Disallow: /
User-agent: AhrefsSiteAudit
Disallow: /
User-agent: SemrushBot
Disallow: /
User-agent: SiteAuditBot
Disallow: /
User-agent: SemrushBot-BA
Disallow: /
User-agent: SemrushBot-SI
Disallow: /
User-agent: SemrushBot-SWA
Disallow: /
User-agent: SemrushBot-CT
Disallow: /
User-agent: SplitSignalBot
Disallow: /
User-agent: SemrushBot-COUB
Disallow: /
User-agent: GPTBot
Disallow: /
User-agent: PetalBot
Disallow: /
User-agent: DotBot
Disallow: /
User-Agent: ImagesiftBot
Disallow: /
`

func (s *Server) handleRobotsTXT() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(robotsTXTContent))
		if err != nil {
			log.Printf("Error writing robots.txt content: %v", err)
		}
	}
}

func (s *Server) handleListMessages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/group/"), "/")
		groupName := pathParts[0]
		var year, month int
		var err error
		if len(pathParts) == 3 && strings.HasSuffix(pathParts[2], ".html") {
			yearStr := pathParts[1]
			monthStr := strings.TrimSuffix(pathParts[2], ".html")
			year, err = strconv.Atoi(yearStr)
			if err != nil { http.Error(w, "Invalid year format", http.StatusBadRequest); return }
			month, err = strconv.Atoi(monthStr)
			if err != nil || month < 1 || month > 12 { http.Error(w, "Invalid month format", http.StatusBadRequest); return }
		} else if len(pathParts) == 2 && pathParts[1] == "" {
			latestYear, latestMonth, found := s.db.GetLatestMessageMonthForGroup(ctx, groupName)
			if !found {
				log.Printf("No latest month found for group %s, will render empty list.", groupName)
				year = 0; month = 0
			} else {
				year = latestYear; month = latestMonth
			}
		} else {
			http.NotFound(w, r); return
		}

		var articles []models.Article
		if year != 0 && month != 0 {
			articles, err = s.db.GetMessagesForGroupMonth(ctx, groupName, year, month)
			if err != nil {
				log.Printf("Error fetching messages for group %s, year %d, month %d: %v", groupName, year, month, err)
				http.Error(w, "Failed to retrieve messages", http.StatusInternalServerError); return
			}
		} else {
			articles = []models.Article{}
		}

		var threadables []jwz.Threadable
		for i := range articles {
			article := &articles[i]
			article.DisplayFrom = utils.ObfuscateEmailInFromHeader(article.From)
			parsedDate, dateParseErr := mail.ParseDate(article.Date)
			if dateParseErr != nil {
				log.Printf("Warning: Could not parse date string '%s' for article %s (ID: %d) for JWZ: %v. Using zero time.", article.Date, article.MessageID, article.ArticleNum, dateParseErr)
				parsedDate = time.Time{}
			} else {
				parsedDate = parsedDate.UTC()
			}
			threadables = append(threadables, &jwzArticleAdapter{ Article: article, ParsedDate: parsedDate })
		}

		var rootDisplayMessages []*DisplayThreadItem
		if len(threadables) > 0 {
			threader := jwz.NewThreader()
			rootThreadable, errThread := threader.ThreadSlice(threadables)
			if errThread != nil {
				log.Printf("Error threading messages for group %s: %v", groupName, errThread)
			} else if rootThreadable != nil {
				rootDisplayMessages, _ = buildDisplayTree(rootThreadable, 0)
				if len(rootDisplayMessages) > 1 {
					sort.SliceStable(rootDisplayMessages, func(i, j int) bool {
						adapterI, okI := rootDisplayMessages[i].Message.(*jwzArticleAdapter)
						adapterJ, okJ := rootDisplayMessages[j].Message.(*jwzArticleAdapter)
						if !okI || !okJ { return rootDisplayMessages[i].Message.MessageThreadID() < rootDisplayMessages[j].Message.MessageThreadID() }
						dateI := adapterI.GetDate(); dateJ := adapterJ.GetDate()
						if !dateI.Equal(dateJ) { return dateI.After(dateJ) }
						return adapterI.MessageThreadID() < adapterJ.MessageThreadID()
					})
				}
			}
		}

		prevYear, prevMonth, prevMonthFound, prevErr := s.db.GetPrevMonthWithMessages(ctx, groupName, year, month)
		if prevErr != nil { log.Printf("Error getting previous month for %s (%d/%d): %v", groupName, year, month, prevErr); prevMonthFound = false }
		nextYear, nextMonth, nextMonthFound, nextErr := s.db.GetNextMonthWithMessages(ctx, groupName, year, month)
		if nextErr != nil { log.Printf("Error getting next month for %s (%d/%d): %v", groupName, year, month, nextErr); nextMonthFound = false }

		data := map[string]interface{}{
			"GroupName": groupName, "ThreadedMessages": rootDisplayMessages, "CurrentYear": year, "CurrentMonth": month,
			"PrevMonthFound": prevMonthFound, "PrevYear": prevYear, "PrevMonth": prevMonth,
			"NextMonthFound": nextMonthFound, "NextYear": nextYear, "NextMonth": nextMonth, "Error": nil,
		}
		s.renderTemplate(w, r, "message_list.html.tmpl", data)
	}
}

func (s *Server) handleShowArticle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		path := r.URL.Path
		trimmedPath := strings.TrimPrefix(path, "/group/")
		parts := strings.Split(trimmedPath, "/")
		var article *models.Article
		var err error
		var groupNameFromPath string
		if len(parts) > 0 { groupNameFromPath = parts[0] }

		var msgIDVal string = r.URL.Query().Get("msgid")
		if msgIDVal == "" && len(parts) >= 2 && strings.HasPrefix(parts[len(parts)-1], ";.msgid=") {
			msgIDVal = strings.TrimPrefix(parts[len(parts)-1], ";.msgid=")
			log.Printf("handleShowArticle: Using path-based ';.msgid=': %s", msgIDVal)
		}
		log.Printf("handleShowArticle: Path='%s', RawQuery='%s', Final ExtractedMsgID='%s', Parts='%v'", path, r.URL.RawQuery, msgIDVal, parts)

		if msgIDVal != "" {
			fullMsgIDWithBrackets := fmt.Sprintf("<%s>", msgIDVal)
			log.Printf("handleShowArticle: Attempting to fetch by Message-ID: '%s' (raw extracted: '%s', group hint: '%s')", fullMsgIDWithBrackets, msgIDVal, groupNameFromPath)
			article, err = s.db.GetArticleByMessageID(ctx, fullMsgIDWithBrackets)
			if err != nil {
				log.Printf("Error fetching article by Message-ID '%s': %v", fullMsgIDWithBrackets, err)
				http.Error(w, "Failed to retrieve article by Message-ID", http.StatusNotFound); return
			}
			if article == nil { log.Printf("Article not found for Message-ID: %s", fullMsgIDWithBrackets); http.NotFound(w, r); return }
			articleReceivedTime := article.Received
			canonicalPath := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", article.GroupName, articleReceivedTime.Year(), articleReceivedTime.Month(), article.ArticleNum)
			log.Printf("Redirecting from Message-ID lookup (original path: %s) to canonical path: %s", path, canonicalPath)
			http.Redirect(w, r, canonicalPath, http.StatusFound); return
		}

		if len(parts) != 4 || !strings.HasPrefix(parts[3], "msg") || !strings.HasSuffix(parts[3], ".html") {
			log.Printf("Invalid canonical article path structure: %s. Parts: %v", path, parts)
			http.NotFound(w, r); return
		}
		reqGroupName := parts[0]
		yearStr, monthStr, msgFile := parts[1], parts[2], parts[3]
		articleNumStr := strings.TrimSuffix(strings.TrimPrefix(msgFile, "msg"), ".html")
		yearFromPath, errYear := strconv.Atoi(yearStr)
		monthFromPath, errMonth := strconv.Atoi(monthStr)
		articleNumFromPath, errNum := strconv.Atoi(articleNumStr)
		if errYear != nil || errMonth != nil || errNum != nil || monthFromPath < 1 || monthFromPath > 12 {
			http.Error(w, "Invalid article identifier in path", http.StatusBadRequest); return
		}

		log.Printf("Fetching article by details: Group=%s, Year=%d, Month=%d, Num=%d", reqGroupName, yearFromPath, monthFromPath, articleNumFromPath)
		retrievedArticle, err := s.db.GetArticleByDetails(ctx, reqGroupName, yearFromPath, monthFromPath, uint32(articleNumFromPath))
		if err != nil {
			log.Printf("Error fetching article by details %s/%d/%d/msg%d: %v", reqGroupName, yearFromPath, monthFromPath, articleNumFromPath, err)
			http.Error(w, "Failed to retrieve article", http.StatusInternalServerError); return
		}
		if retrievedArticle == nil { log.Printf("Article not found by details: %s/%d/%d/msg%d", reqGroupName, yearFromPath, monthFromPath, articleNumFromPath); http.NotFound(w, r); return }
		article = retrievedArticle

		articleActualYear := article.Received.Year()
		articleActualMonth := int(article.Received.Month())
		if articleActualYear != yearFromPath || articleActualMonth != monthFromPath {
			canonicalPath := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html", article.GroupName, articleActualYear, articleActualMonth, article.ArticleNum)
			log.Printf("Redirecting from incorrect date path (URL: %d/%d, Article: %d/%d) to canonical: %s", yearFromPath, monthFromPath, articleActualYear, articleActualMonth, canonicalPath)
			http.Redirect(w, r, canonicalPath, http.StatusFound); return
		}

		rawArticleBytes, err := s.nntpClient.FetchRawArticle(ctx, article.GroupName, article.ArticleNum)
		if err != nil {
			log.Printf("Error fetching raw article body for %s/msg%d from NNTP: %v", article.GroupName, article.ArticleNum, err)
			http.Error(w, fmt.Sprintf("Failed to fetch article content for %s/msg%d from NNTP server", article.GroupName, article.ArticleNum), http.StatusInternalServerError); return
		}

		parsedResult, parseErr := mimeparser.ParseArticle(bytes.NewReader(rawArticleBytes))
		if parseErr != nil {
			log.Printf("Error parsing article content for %s/msg%d: %v", article.GroupName, article.ArticleNum, parseErr)
			http.Error(w, "Failed to parse article content", http.StatusInternalServerError); return
		}
		parsedContent := parsedResult.PreferredBody
		isHTML := parsedResult.IsHTML
		otherPartsExist := parsedResult.OtherPartsExist

		threadMessages, err := s.db.GetThreadMessages(ctx, article.ThreadID, article.GroupID)
		if err != nil {
			log.Printf("Error fetching thread messages for article %s/msg%d (threadID %d, groupID %d): %v", article.GroupName, article.ArticleNum, article.ThreadID, article.GroupID, err)
			threadMessages = []models.Article{}
		}

		article.DisplayFrom = utils.ObfuscateEmailInFromHeader(article.From)
		for i := range threadMessages { threadMessages[i].DisplayFrom = utils.ObfuscateEmailInFromHeader(threadMessages[i].From) }

		var articleThreadables []jwz.Threadable
		for i := range threadMessages {
			msg := &threadMessages[i]
			parsedDate, dateParseErr := mail.ParseDate(msg.Date)
			if dateParseErr != nil {
				log.Printf("Warning: Could not parse date string '%s' for article %s (ID: %d) in thread view: %v. Using zero time.", msg.Date, msg.MessageID, msg.ArticleNum, dateParseErr)
				parsedDate = time.Time{}
			} else { parsedDate = parsedDate.UTC() }
			articleThreadables = append(articleThreadables, &jwzArticleAdapter{ Article: msg, ParsedDate: parsedDate })
		}

		var articleThreadTree []*DisplayThreadItem
		if len(articleThreadables) > 0 {
			threader := jwz.NewThreader()
			rootThreadable, errThread := threader.ThreadSlice(articleThreadables)
			if errThread != nil {
				log.Printf("Error threading messages for article view %s: %v", article.MessageID, errThread)
			} else if rootThreadable != nil {
				articleThreadTree, _ = buildDisplayTree(rootThreadable, 0)
			}
		}

		var articleContentHTML template.HTML
		if isHTML { articleContentHTML = template.HTML(parsedContent) }

		var displayDateUTC string
		parsedArticleHeaderDate, errParseDate := mail.ParseDate(article.Date)
		if errParseDate != nil {
			log.Printf("Warning: Could not parse main article date string '%s' for UTC display: %v. Using original string.", article.Date, errParseDate)
			displayDateUTC = article.Date
		} else {
			displayDateUTC = parsedArticleHeaderDate.UTC().Format("2006-01-02 15:04:05 UTC")
		}

		data := map[string]interface{}{
			"Article": article, "DisplayDateUTC": displayDateUTC, "ArticleContent": parsedContent,
			"ArticleContentHTML": articleContentHTML, "IsHTMLContent": isHTML, "OtherPartsExist": otherPartsExist,
			"ArticleThreadTree": articleThreadTree, "GroupName": article.GroupName,
			"CurrentYear": articleActualYear, "CurrentMonth": articleActualMonth,
		}
		s.renderTemplate(w, r, "article.html.tmpl", data)
	}
}
