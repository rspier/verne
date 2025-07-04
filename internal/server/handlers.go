package server

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail" // Added for mail.ParseDate
	"strconv"
	"bytes" // Added for bytes.NewReader
	"strings"
	"time"

	"nntp-web/internal/mimeparser" // Added for mimeparser.ParseArticle
	"nntp-web/internal/models"
	"nntp-web/internal/utils"    // For ObfuscateEmailInFromHeader
	"github.com/gatherstars-com/jwz" // Corrected JWZ threading import
)

// handleListGroups displays the list of all newsgroups.
// It needs to be a method on *Server to access s.db and s.renderTemplate
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

// handleListMessages displays messages for a given group, year, and month.
// It handles both /group/{groupname}/ (current month) and /group/{groupname}/{year}/{month}.html
func (s *Server) handleListMessages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/group/"), "/")
		groupName := pathParts[0]

		var year, month int
		var err error

		if len(pathParts) == 3 && strings.HasSuffix(pathParts[2], ".html") { // /group/{groupname}/{year}/{month}.html
			yearStr := pathParts[1]
			monthStr := strings.TrimSuffix(pathParts[2], ".html")
			year, err = strconv.Atoi(yearStr)
			if err != nil {
				http.Error(w, "Invalid year format", http.StatusBadRequest)
				return
			}
			month, err = strconv.Atoi(monthStr)
			if err != nil {
				http.Error(w, "Invalid month format", http.StatusBadRequest)
				return
			}
		} else if len(pathParts) == 2 && pathParts[1] == "" { // /group/{groupname}/ (current month)
			// Determine latest year/month with messages for this group
			latestYear, latestMonth, found := s.db.GetLatestMessageMonthForGroup(groupName)
			if !found {
				// Render with "no messages" or handle as appropriate
				// For now, let it proceed, GetMessagesForGroupMonth might return empty
				// and template handles "No messages found".
				// Or, redirect to a "no content" page or show a specific message.
				// Let's use current year/month as a fallback, though likely empty.
				log.Printf("No latest month found for group %s, defaulting to current system month", groupName)
				now := time.Now()
				year = now.Year()
				month = int(now.Month())
			} else {
				year = latestYear
				month = latestMonth
			}
		} else {
			http.NotFound(w, r)
			return
		}

		// Fetch articles for the group and month/year
		articles, err := s.db.GetMessagesForGroupMonth(groupName, year, month)
		if err != nil {
			log.Printf("Error fetching messages for group %s, year %d, month %d: %v", groupName, year, month, err)
			http.Error(w, "Failed to retrieve messages", http.StatusInternalServerError)
			return
		}

		// Prepare for JWZ threading
		jwzMessages := make([]jwz.Message, len(articles))
		for i, article := range articles {
			article.DisplayFrom = utils.ObfuscateEmailInFromHeader(article.From)
			// Use mail.ParseDate for robust parsing of h_date (article.Date)
			parsedDate, dateParseErr := mail.ParseDate(article.Date)
			if dateParseErr != nil {
				log.Printf("Warning: Could not parse date string '%s' for article %s (ID: %d) for JWZ: %v. Using zero time.", article.Date, article.MessageID, article.ArticleNum, dateParseErr)
				parsedDate = time.Time{} // JWZ library might handle zero times gracefully or not.
			}
			jwzMessages[i] = &jwzArticleAdapter{
				Article:    &articles[i], // Use address of the article from the slice
				ParsedDate: parsedDate,
			}
		}
		threadedMessages := jwz.Thread(jwzMessages)

		// Previous/Next month navigation
		prevYear, prevMonth, prevMonthFound, prevErr := s.db.GetPrevMonthWithMessages(groupName, year, month)
		if prevErr != nil {
			log.Printf("Error getting previous month for %s (%d/%d): %v", groupName, year, month, prevErr)
			// Non-fatal, just won't show the link
			prevMonthFound = false
		}
		nextYear, nextMonth, nextMonthFound, nextErr := s.db.GetNextMonthWithMessages(groupName, year, month)
		if nextErr != nil {
			log.Printf("Error getting next month for %s (%d/%d): %v", groupName, year, month, nextErr)
			// Non-fatal, just won't show the link
			nextMonthFound = false
		}

		data := map[string]interface{}{
			"GroupName":        groupName,
			"ThreadedMessages": threadedMessages,
			"CurrentYear":      year,
			"CurrentMonth":     month,
			"PrevMonthFound":   prevMonthFound,
			"PrevYear":         prevYear,
			"PrevMonth":        prevMonth,
			"NextMonthFound":   nextMonthFound,
			"NextYear":         nextYear,
			"NextMonth":        nextMonth,
			"Error":            nil, // Placeholder for any errors to display
		}
		s.renderTemplate(w, r, "message_list.html.tmpl", data)
	}
}

// handleShowArticle displays a single article.
// It handles /group/{groupname}/{year}/{month}/msg{id}.html and /group/{groupname}/;.msgid={messageid}
func (s *Server) handleShowArticle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		trimmedPath := strings.TrimPrefix(path, "/group/")
		parts := strings.Split(trimmedPath, "/") // e.g. ["group", "year", "month", "msgID.html"] or ["group", ";.msgid=..."]

		var article *models.Article
		var err error

		groupName := parts[0]

		// Attempt to get by ;.msgid= first if present
		msgIDVal := r.URL.Query().Get(";.msgid")
		if msgIDVal != "" { // This is how it's typically passed, not as part of path usually
			// If it was part of path like /group/foo/;.msgid=bar
			// parts would be ["foo", ";.msgid=bar"]
			if len(parts) == 2 && strings.HasPrefix(parts[1], ";.msgid=") {
				// msgIDValFromPath := strings.TrimPrefix(parts[1], ";.msgid=")
				// if msgIDValFromPath != "" { msgIDVal = msgIDValFromPath } // Prefer query param if both somehow exist
				// No, the router logic in server.go already handles this specific path structure.
				// We should just use the query parameter if the router directed here for ;.msgid=
			}
			// Re-check query param as primary source
			msgIDVal = r.URL.Query().Get(";.msgid") // Ensure we have it from query
			if msgIDVal == "" && len(parts) == 2 && strings.HasPrefix(parts[1], ";.msgid="){ // Fallback if it was in path
				msgIDVal = strings.TrimPrefix(parts[1], ";.msgid=")
			}


			if msgIDVal != "" {
				log.Printf("Attempting to fetch article by Message-ID: %s (group hint: %s)", msgIDVal, groupName)
				article, err = s.db.GetArticleByMessageID(msgIDVal)
				if err != nil {
					log.Printf("Error fetching article by Message-ID %s: %v", msgIDVal, err)
					http.Error(w, "Failed to retrieve article by Message-ID", http.StatusNotFound)
					return
				}
				if article == nil {
					http.NotFound(w, r)
					return
				}
				// Redirect to canonical URL
				// article.Received is time.Time, from h_date_received
				canonicalPath := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html",
					article.GroupName, article.Received.Year(), article.Received.Month(), article.ArticleNum)
				log.Printf("Redirecting from ;.msgid lookup to canonical path: %s", canonicalPath)
				http.Redirect(w, r, canonicalPath, http.StatusFound)
				return
			}
		}

		// Canonical path: /group/{groupname}/{year}/{month}/msg{id}.html
		// parts: ["groupname", "year", "month", "msg<id>.html"]
		if len(parts) != 4 || !strings.HasPrefix(parts[3], "msg") || !strings.HasSuffix(parts[3], ".html") {
			log.Printf("Invalid article path structure: %s", path)
			http.NotFound(w, r)
			return
		}

		yearStr, monthStr, msgFile := parts[1], parts[2], parts[3]
		articleNumStr := strings.TrimSuffix(strings.TrimPrefix(msgFile, "msg"), ".html")

		year, errYear := strconv.Atoi(yearStr)
		month, errMonth := strconv.Atoi(monthStr)
		articleNum, errNum := strconv.Atoi(articleNumStr)

		if errYear != nil || errMonth != nil || errNum != nil {
			http.Error(w, "Invalid article identifier in path", http.StatusBadRequest)
			return
		}

		log.Printf("Fetching article by details: Group=%s, Year=%d, Month=%d, Num=%d", groupName, year, month, articleNum)
		// Use GetArticleByDetails which can handle date mismatches for redirects
		retrievedArticle, err := s.db.GetArticleByDetails(groupName, year, month, uint32(articleNum))
		if err != nil {
			log.Printf("Error fetching article by details %s/%d/%d/msg%d: %v", groupName, year, month, articleNum, err)
			http.Error(w, "Failed to retrieve article", http.StatusInternalServerError)
			return
		}
		if retrievedArticle == nil {
			http.NotFound(w, r)
			return
		}
		article = retrievedArticle

		// Check if the requested year/month in path matches the article's actual date. Redirect if not.
		// article.Received is time.Time from h_date_received
		articleActualYear := article.Received.Year()
		articleActualMonth := int(article.Received.Month())
		if articleActualYear != year || articleActualMonth != month { // year and month are from path
			canonicalPath := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html",
				article.GroupName, articleActualYear, articleActualMonth, article.ArticleNum)
			log.Printf("Redirecting from incorrect date path (URL: %d/%d, Article: %d/%d) to canonical: %s", year, month, articleActualYear, articleActualMonth, canonicalPath)
			http.Redirect(w, r, canonicalPath, http.StatusFound)
			return
		}

		// Fetch raw article body from NNTP server
		rawArticleBytes, err := s.nntpClient.FetchRawArticle(article.GroupName, article.ArticleNum)
		if err != nil {
			log.Printf("Error fetching raw article body for %s/msg%d from NNTP: %v", article.GroupName, article.ArticleNum, err)
			http.Error(w, "Failed to fetch article content from NNTP server", http.StatusInternalServerError)
			return
		}

		// Parse and sanitize the article content
		parsedResult, parseErr := mimeparser.ParseArticle(bytes.NewReader(rawArticleBytes))
		if parseErr != nil {
			log.Printf("Error parsing article content for %s/msg%d: %v", article.GroupName, article.ArticleNum, parseErr)
			http.Error(w, "Failed to parse article content", http.StatusInternalServerError)
			return
		}
		parsedContent := parsedResult.PreferredBody
		isHTML := parsedResult.IsHTML
		otherPartsExist := parsedResult.OtherPartsExist


		// Fetch messages in the same thread (excluding the current one)
		threadMessages, err := s.db.GetThreadMessages(article.ThreadID, article.GroupID, article.ArticleNum)
		if err != nil {
			log.Printf("Error fetching thread messages for article %s/msg%d (threadID %d): %v", article.GroupName, article.ArticleNum, article.ThreadID, err)
			// Non-critical, so we can proceed without thread messages if there's an error
			threadMessages = []*models.Article{} // Empty slice
		}

		// Obfuscate emails for display
		article.DisplayFrom = utils.ObfuscateEmailInFromHeader(article.From)
		for _, tm := range threadMessages {
			tm.DisplayFrom = utils.ObfuscateEmailInFromHeader(tm.From)
		}


		var articleContentHTML template.HTML
		if isHTML {
			articleContentHTML = template.HTML(parsedContent)
		}


		data := map[string]interface{}{
			"Article":          article,
			"ArticleContent":   parsedContent, // For <pre> if not HTML
			"ArticleContentHTML": articleContentHTML, // For direct rendering if HTML
			"IsHTMLContent":    isHTML,
			"OtherPartsExist":  otherPartsExist,
			"ThreadMessages":   threadMessages,
			"GroupName":        article.GroupName, // For breadcrumbs or links
			"CurrentYear":      articleActualYear, // Use the actual year from article.Received
			"CurrentMonth":     articleActualMonth, // Use the actual month from article.Received
		}
		s.renderTemplate(w, r, "article.html.tmpl", data)
	}
}
