package server

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	// "net/url" // No longer needed
	"sort" // For sorting thread lists
	"strconv"
	"strings"
	"time"

	"nntp-web/internal/mimeparser"
	"nntp-web/internal/models"
	"nntp-web/internal/utils"
	"github.com/gatherstars-com/jwz"
)

// DisplayThreadItem is used for rendering the threaded message list.
type DisplayThreadItem struct {
	Message                   jwz.Threadable
	Depth                     int
	Children                  []*DisplayThreadItem
	MessagesInSubThreadCount int // New field: count of messages in this item's thread/sub-thread
}

// jwzArticleAdapter is defined in server.go, this redundant definition is removed.

// The incorrect buildDisplayTree definition has been removed.
// The correct one below remains.

// buildDisplayTree processes a list of sibling Threadable messages (linked by GetNext)
// and recursively builds a tree of DisplayThreadItem nodes.
// It returns the list of DisplayThreadItem nodes created at the current level,
// and the total count of messages in the subtrees rooted at these nodes.
func buildDisplayTree(rootContainer jwz.Threadable, currentDepth int) ([]*DisplayThreadItem, int) {
	var items []*DisplayThreadItem
	var totalMessagesInSubTree int = 0
	sibling := rootContainer

	for sibling != nil {
		if !sibling.IsDummy() {
			item := &DisplayThreadItem{
				Message: sibling,
				Depth:   currentDepth,
				// MessagesInSubThreadCount will be calculated below
			}

			messagesInThisBranch := 1 // Count the current message itself

			if child := sibling.GetChild(); child != nil {
				var childrenItems []*DisplayThreadItem
				var countFromChildren int
				// Recursively call buildDisplayTree for children
				childrenItems, countFromChildren = buildDisplayTree(child, currentDepth+1)
				item.Children = childrenItems
				messagesInThisBranch += countFromChildren
			}

			item.MessagesInSubThreadCount = messagesInThisBranch
			items = append(items, item)
			totalMessagesInSubTree += messagesInThisBranch
		} else {
			// If the current sibling is a dummy, we don't create a DisplayThreadItem for it.
			// However, its children should be processed as if they are at the current depth,
			// effectively promoting them. The jwz library sometimes produces a dummy root
			// whose children are the actual top-level threads.
			log.Printf("Skipping dummy item: %s, processing its children at current depth %d.", sibling.MessageThreadID(), currentDepth)
			if child := sibling.GetChild(); child != nil {
				// Process children of the dummy. These children become part of the current list of items.
				childrenOfDummy, countFromDummyChildren := buildDisplayTree(child, currentDepth) // Children are at the same depth as dummy's original level.
				items = append(items, childrenOfDummy...) // Add children directly to the current list
				totalMessagesInSubTree += countFromDummyChildren
			}
		}
		sibling = sibling.GetNext()
	}
	return items, totalMessagesInSubTree
}


func (s *Server) handleListGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups, err := s.db.GetAllNewsgroups()
		if err != nil {
			log.Printf("Error fetching newsgroups: %v", err)
			http.Error(w, "Failed to retrieve newsgroups", http.StatusInternalServerError)
			return
		}
		s.renderTemplate(w, r, "groups.html.tmpl", map[string]interface{}{"Groups": groups})
	}
}

func (s *Server) handleListMessages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/group/"), "/")
		groupName := pathParts[0]
		var year, month int
		var err error

		if len(pathParts) == 3 && strings.HasSuffix(pathParts[2], ".html") {
			yearStr := pathParts[1]
			monthStr := strings.TrimSuffix(pathParts[2], ".html")
			year, err = strconv.Atoi(yearStr)
			if err != nil {
				http.Error(w, "Invalid year format", http.StatusBadRequest)
				return
			}
			month, err = strconv.Atoi(monthStr)
			if err != nil || month < 1 || month > 12 {
				http.Error(w, "Invalid month format", http.StatusBadRequest)
				return
			}
		} else if len(pathParts) == 2 && pathParts[1] == "" {
			latestYear, latestMonth, found := s.db.GetLatestMessageMonthForGroup(groupName)
			if !found {
				log.Printf("No latest month found for group %s, will render empty list.", groupName)
				year = 0
				month = 0
			} else {
				year = latestYear
				month = latestMonth
			}
		} else {
			http.NotFound(w, r)
			return
		}

		var articles []models.Article
		if year != 0 && month != 0 {
			articles, err = s.db.GetMessagesForGroupMonth(groupName, year, month)
			if err != nil {
				log.Printf("Error fetching messages for group %s, year %d, month %d: %v", groupName, year, month, err)
				http.Error(w, "Failed to retrieve messages", http.StatusInternalServerError)
				return
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
			}
			threadables = append(threadables, &jwzArticleAdapter{
				Article:    article,
				ParsedDate: parsedDate,
			})
		}

		var rootDisplayMessages []*DisplayThreadItem
		if len(threadables) > 0 {
			threader := jwz.NewThreader()
			rootThreadable, errThread := threader.ThreadSlice(threadables)
			if errThread != nil {
				log.Printf("Error threading messages for group %s: %v", groupName, errThread)
			} else if rootThreadable != nil {
				// buildDisplayTree now returns (items, totalCount). We only need items here.
				rootDisplayMessages, _ = buildDisplayTree(rootThreadable, 0)

				// Removed explicit reversal. Rely on jwz order or implement specific sort if needed.
				// The expectation is that threads should be listed newest first.
				// The input articles are newest first (DB: received DESC).
				// If jwz outputs thread roots oldest-first, this will result in oldest-first display.
				// If jwz outputs thread roots newest-first, this will be correct.
				// If order is still wrong, a manual sort of rootDisplayMessages by date will be needed here.

				// Implement stable sort: newest threads first.
				if len(rootDisplayMessages) > 1 {
					sort.SliceStable(rootDisplayMessages, func(i, j int) bool {
						adapterI, okI := rootDisplayMessages[i].Message.(*jwzArticleAdapter)
						adapterJ, okJ := rootDisplayMessages[j].Message.(*jwzArticleAdapter)

						// If type assertion fails, fallback to a stable sort based on MessageID only,
						// or handle as an error. For now, non-adapters sort consistently but arbitrarily first.
						if !okI || !okJ {
							// This case implies that DisplayThreadItem.Message is not always *jwzArticleAdapter
							// which would be unexpected if buildDisplayTree only populates with non-dummy adapters.
							// Fallback to sorting by MessageID to maintain stability.
							// Or, if this is truly an error state, log it.
							// log.Printf("Warning: Unexpected type in Message field during sort: %T, %T", rootDisplayMessages[i].Message, rootDisplayMessages[j].Message)
							return rootDisplayMessages[i].Message.MessageThreadID() < rootDisplayMessages[j].Message.MessageThreadID()
						}

						dateI := adapterI.GetDate()
						dateJ := adapterJ.GetDate()

						if !dateI.Equal(dateJ) {
							return dateI.After(dateJ) // Newest first
						}

						// Secondary sort: MessageID of root message, ascending (for stability)
						return adapterI.MessageThreadID() < adapterJ.MessageThreadID()
					})
				}
			}
		}

		prevYear, prevMonth, prevMonthFound, prevErr := s.db.GetPrevMonthWithMessages(groupName, year, month)
		if prevErr != nil {
			log.Printf("Error getting previous month for %s (%d/%d): %v", groupName, year, month, prevErr)
			prevMonthFound = false
		}
		nextYear, nextMonth, nextMonthFound, nextErr := s.db.GetNextMonthWithMessages(groupName, year, month)
		if nextErr != nil {
			log.Printf("Error getting next month for %s (%d/%d): %v", groupName, year, month, nextErr)
			nextMonthFound = false
		}

		data := map[string]interface{}{
			"GroupName":        groupName,
			"ThreadedMessages": rootDisplayMessages,
			"CurrentYear":      year,
			"CurrentMonth":     month,
			"PrevMonthFound":   prevMonthFound,
			"PrevYear":         prevYear,
			"PrevMonth":        prevMonth,
			"NextMonthFound":   nextMonthFound,
			"NextYear":         nextYear,
			"NextMonth":        nextMonth,
			"Error":            nil,
		}
		s.renderTemplate(w, r, "message_list.html.tmpl", data)
	}
}

func (s *Server) handleShowArticle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		trimmedPath := strings.TrimPrefix(path, "/group/")
		parts := strings.Split(trimmedPath, "/")

		var article *models.Article
		var err error
		var groupNameFromPath string
		if len(parts) > 0 {
			groupNameFromPath = parts[0]
		}

		var msgIDVal string
		// Prefer "msgid" query parameter
		msgIDVal = r.URL.Query().Get("msgid")

		// Fallback to path-based ";.msgid=" if router sent it here for that reason
		// The router in server.go also has logic for path-based ;.msgid=
		if msgIDVal == "" && len(parts) >= 2 && strings.HasPrefix(parts[len(parts)-1], ";.msgid=") {
			pathSegmentMsgID := strings.TrimPrefix(parts[len(parts)-1], ";.msgid=")
			msgIDVal = pathSegmentMsgID
			log.Printf("handleShowArticle: Using path-based ';.msgid=': %s", msgIDVal)
		}

		log.Printf("handleShowArticle: Path='%s', RawQuery='%s', Final ExtractedMsgID='%s', Parts='%v'", path, r.URL.RawQuery, msgIDVal, parts)

		if msgIDVal != "" {
			log.Printf("handleShowArticle: Attempting to fetch by Message-ID: '%s' (group hint: '%s')", msgIDVal, groupNameFromPath)
			article, err = s.db.GetArticleByMessageID(msgIDVal)
			if err != nil {
				log.Printf("Error fetching article by Message-ID '%s': %v", msgIDVal, err)
				http.Error(w, "Failed to retrieve article by Message-ID", http.StatusNotFound)
				return
			}
			if article == nil {
				log.Printf("Article not found for Message-ID: %s", msgIDVal)
				http.NotFound(w, r)
				return
			}

			articleReceivedTime := article.Received
			canonicalPath := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html",
				article.GroupName, articleReceivedTime.Year(), articleReceivedTime.Month(), article.ArticleNum)
			log.Printf("Redirecting from Message-ID lookup (original path: %s) to canonical path: %s", path, canonicalPath)
			http.Redirect(w, r, canonicalPath, http.StatusFound)
			return
		}

		if len(parts) != 4 || !strings.HasPrefix(parts[3], "msg") || !strings.HasSuffix(parts[3], ".html") {
			log.Printf("Invalid canonical article path structure: %s. Parts: %v", path, parts)
			http.NotFound(w, r)
			return
		}

		reqGroupName := parts[0]
		yearStr, monthStr, msgFile := parts[1], parts[2], parts[3]
		articleNumStr := strings.TrimSuffix(strings.TrimPrefix(msgFile, "msg"), ".html")

		yearFromPath, errYear := strconv.Atoi(yearStr)
		monthFromPath, errMonth := strconv.Atoi(monthStr)
		articleNumFromPath, errNum := strconv.Atoi(articleNumStr)

		if errYear != nil || errMonth != nil || errNum != nil || monthFromPath < 1 || monthFromPath > 12 {
			http.Error(w, "Invalid article identifier in path (year, month, or number format/range)", http.StatusBadRequest)
			return
		}

		log.Printf("Fetching article by details: Group=%s, Year=%d, Month=%d, Num=%d", reqGroupName, yearFromPath, monthFromPath, articleNumFromPath)
		retrievedArticle, err := s.db.GetArticleByDetails(reqGroupName, yearFromPath, monthFromPath, uint32(articleNumFromPath))
		if err != nil {
			log.Printf("Error fetching article by details %s/%d/%d/msg%d: %v", reqGroupName, yearFromPath, monthFromPath, articleNumFromPath, err)
			http.Error(w, "Failed to retrieve article", http.StatusInternalServerError)
			return
		}
		if retrievedArticle == nil {
			log.Printf("Article not found by details: %s/%d/%d/msg%d", reqGroupName, yearFromPath, monthFromPath, articleNumFromPath)
			http.NotFound(w, r)
			return
		}
		article = retrievedArticle

		articleActualYear := article.Received.Year()
		articleActualMonth := int(article.Received.Month())
		if articleActualYear != yearFromPath || articleActualMonth != monthFromPath {
			canonicalPath := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html",
				article.GroupName, articleActualYear, articleActualMonth, article.ArticleNum)
			log.Printf("Redirecting from incorrect date path (URL: %d/%d, Article: %d/%d) to canonical: %s", yearFromPath, monthFromPath, articleActualYear, articleActualMonth, canonicalPath)
			http.Redirect(w, r, canonicalPath, http.StatusFound)
			return
		}

		rawArticleBytes, err := s.nntpClient.FetchRawArticle(article.GroupName, article.ArticleNum)
		if err != nil {
			log.Printf("Error fetching raw article body for %s/msg%d from NNTP: %v", article.GroupName, article.ArticleNum, err)
			http.Error(w, fmt.Sprintf("Failed to fetch article content for %s/msg%d from NNTP server", article.GroupName, article.ArticleNum), http.StatusInternalServerError)
			return
		}

		parsedResult, parseErr := mimeparser.ParseArticle(bytes.NewReader(rawArticleBytes))
		if parseErr != nil {
			log.Printf("Error parsing article content for %s/msg%d: %v", article.GroupName, article.ArticleNum, parseErr)
			http.Error(w, "Failed to parse article content", http.StatusInternalServerError)
			return
		}
		parsedContent := parsedResult.PreferredBody
		isHTML := parsedResult.IsHTML
		otherPartsExist := parsedResult.OtherPartsExist

		// Fetch all messages in the thread, including the current one.
		// GetThreadMessages signature changed: no longer needs currentArticleGroupID, currentArticleNum
		threadMessages, err := s.db.GetThreadMessages(article.ThreadID)
		if err != nil {
			log.Printf("Error fetching thread messages for article %s/msg%d (threadID %d): %v", article.GroupName, article.ArticleNum, article.ThreadID, err)
			threadMessages = []models.Article{}
		}

		article.DisplayFrom = utils.ObfuscateEmailInFromHeader(article.From)
		for i := range threadMessages {
			threadMessages[i].DisplayFrom = utils.ObfuscateEmailInFromHeader(threadMessages[i].From)
		}

		// Prepare threadables for JWZ processing for the article's thread display
		var articleThreadables []jwz.Threadable
		for i := range threadMessages { // These are already "other" messages
			msg := &threadMessages[i] // Use a pointer to the item in the slice
			// Ensure DisplayFrom is set for these messages as well
			// msg.DisplayFrom is already set in the loop above.

			parsedDate, dateParseErr := mail.ParseDate(msg.Date)
			if dateParseErr != nil {
				log.Printf("Warning: Could not parse date string '%s' for article %s (ID: %d) in thread view: %v. Using zero time.", msg.Date, msg.MessageID, msg.ArticleNum, dateParseErr)
				parsedDate = time.Time{}
			}
			articleThreadables = append(articleThreadables, &jwzArticleAdapter{
				Article:    msg,
				ParsedDate: parsedDate,
			})
		}

		var articleThreadTree []*DisplayThreadItem
		if len(articleThreadables) > 0 {
			threader := jwz.NewThreader()
			// Note: jwz.Containerize might be needed if ThreadSlice expects a specific root or if messages are disparate.
			// For a simple list of messages belonging to the same thread (excluding the current one),
			// ThreadSlice should be able to form sub-threads.
			rootThreadable, errThread := threader.ThreadSlice(articleThreadables)
			if errThread != nil {
				log.Printf("Error threading messages for article view %s: %v", article.MessageID, errThread)
			} else if rootThreadable != nil {
				// Pass depth 0 as these are the roots of the "other messages" display
				articleThreadTree, _ = buildDisplayTree(rootThreadable, 0) // Ignore the count for this specific tree
			}
		}

		var articleContentHTML template.HTML
		if isHTML {
			articleContentHTML = template.HTML(parsedContent)
		}

		data := map[string]interface{}{
			"Article":            article,
			"ArticleContent":     parsedContent,
			"ArticleContentHTML": articleContentHTML,
			"IsHTMLContent":      isHTML,
			"OtherPartsExist":    otherPartsExist,
			"ArticleThreadTree":  articleThreadTree, // New field for the template
			"GroupName":          article.GroupName,
			"CurrentYear":        articleActualYear,
			"CurrentMonth":       articleActualMonth,
		}
		s.renderTemplate(w, r, "article.html.tmpl", data)
	}
}
