package server

import (
	"database/sql" // For sql.ErrNoRows
	"errors"       // For errors.Is
	"fmt"          // For fmt.Sprintf
	"log"
	"net/http"
	"nntp-web/web/templates" // For template name constants
	"strconv"      // For strconv.Atoi, strconv.ParseUint
	"strings"      // For strings.Split, strings.Trim, etc.
	"time"         // For time.Now, time.Date
)

// handleListGroups handles requests to list all newsgroups.
func (s *Server) handleListGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The determination that this handler should run (i.e., path is exactly /group/)
		// is now done by the routeGroupRequests dispatcher.
		groups, err := s.db.GetAllNewsgroups()
		if err != nil {
			log.Printf("Error getting all newsgroups: %v", err)
			// In a real app, you might render an error page here
			// For now, sending a simple error response.
			// s.renderTemplate(w, r, templates.ErrorPage, map[string]interface{}{"Error": "Could not load newsgroups."})
			http.Error(w, "Failed to retrieve newsgroups", http.StatusInternalServerError)
			return
		}

		data := map[string]interface{}{
			"Groups": groups,
		}
		s.renderTemplate(w, r, templates.GroupsList, data)
	}
}

// handleListMessages handles requests to list messages for a group,
// defaulting to the current month or a specific year/month.
// Path: /group/{groupname}/
// Path: /group/{groupname}/{year}/{month}.html
func (s *Server) handleListMessages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// Expected: "group", groupNameStr
		// Or: "group", groupNameStr, yearStr, monthStrWithExt
		if len(pathParts) < 2 || pathParts[0] != "group" {
			http.NotFound(w, r)
			return
		}
		groupNameStr := pathParts[1]

		var targetYear, targetMonth int
		var specificMonthRequested bool

		if len(pathParts) == 4 && strings.HasSuffix(pathParts[3], ".html") {
			// Path: /group/{groupname}/{year}/{month}.html
			yearStr := pathParts[2]
			monthStr := strings.TrimSuffix(pathParts[3], ".html")

			var errY, errM error
			targetYear, errY = strconv.Atoi(yearStr)
			targetMonth, errM = strconv.Atoi(monthStr)

			if errY != nil || errM != nil || targetMonth < 1 || targetMonth > 12 {
				log.Printf("Invalid year/month in path: %s/%s", yearStr, monthStr)
				http.Error(w, "Invalid date format in URL.", http.StatusBadRequest)
				return
			}
			specificMonthRequested = true
		} else if len(pathParts) == 2 {
			// Path: /group/{groupname}/ -> current month
			now := time.Now()
			targetYear, targetMonth = now.Year(), int(now.Month())
			specificMonthRequested = false // Using current month
		} else {
			http.NotFound(w, r)
			return
		}

		articles, err := s.db.GetMessagesForGroupMonth(groupNameStr, targetYear, targetMonth)
		if err != nil {
			if strings.Contains(err.Error(), "not found") { // Check if group not found
				log.Printf("Group not found: %s. Error: %v", groupNameStr, err)
				http.Error(w, fmt.Sprintf("Group %s not found.", groupNameStr), http.StatusNotFound)
			} else {
				log.Printf("Error getting messages for group %s, %d-%d: %v", groupNameStr, targetYear, targetMonth, err)
				http.Error(w, "Failed to retrieve messages.", http.StatusInternalServerError)
			}
			return
		}

		// Calculate previous and next months for navigation
		currentDate := time.Date(targetYear, time.Month(targetMonth), 1, 0, 0, 0, 0, time.UTC)
		prevDate := currentDate.AddDate(0, -1, 0)
		nextDate := currentDate.AddDate(0, 1, 0)

		data := map[string]interface{}{
			"GroupName":              groupNameStr,
			"Articles":               articles,
			"CurrentYear":            targetYear,
			"CurrentMonth":           targetMonth,
			"PrevYear":               prevDate.Year(),
			"PrevMonth":              int(prevDate.Month()),
			"NextYear":               nextDate.Year(),
			"NextMonth":              int(nextDate.Month()),
			"SpecificMonthRequested": specificMonthRequested, // To adjust breadcrumbs or titles if needed
		}
		s.renderTemplate(w, r, templates.MessageList, data)
	}
}

// Add other handlers here as the application grows
// e.g., handleShowArticle etc.

// handleShowArticle displays a single article.
// It handles paths like:
// - /group/{groupname}/{year}/{month}/msg{id}.html (canonical)
// - /group/{groupname}/;.msgid={messageid} (lookup by Message-ID)
func (s *Server) handleShowArticle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Path parts relative to / (e.g., ["group", "groupname", ";.msgid=value"] or ["group", "groupname", "Y", "M", "msgN.html"])
		pathSegs := strings.Split(strings.Trim(path, "/"), "/")

		// Type 1: Path-based Message-ID lookup: /group/{groupname}/;.msgid={messageid}
		// pathSegs: ["group", "groupname", ";.msgid=value"]
		if len(pathSegs) == 3 && pathSegs[0] == "group" && strings.HasPrefix(pathSegs[2], ";.msgid=") {
			// groupNameFromPath := pathSegs[1] // Available if needed for context, but GetArticleByMessageID is global
			msgIDVal := strings.TrimPrefix(pathSegs[2], ";.msgid=")

			if msgIDVal == "" {
				http.Error(w, "Missing Message-ID for lookup.", http.StatusBadRequest)
				return
			}

			article, err := s.db.GetArticleByMessageID(msgIDVal)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "not found") {
					log.Printf("Article with Message-ID '%s' not found (path lookup). Error: %v", msgIDVal, err)
					http.Error(w, fmt.Sprintf("Article with Message-ID %s not found.", msgIDVal), http.StatusNotFound)
				} else {
					log.Printf("Error fetching article by Message-ID '%s' (path lookup): %v", msgIDVal, err)
					http.Error(w, "Failed to retrieve article by Message-ID.", http.StatusInternalServerError)
				}
				return
			}

			canonicalURL := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html",
				article.GroupName,
				article.Received.Year(),
				article.Received.Month(),
				article.ArticleNum,
			)
			http.Redirect(w, r, canonicalURL, http.StatusFound)
			return
		}

		// Type 2: Canonical path: /group/{groupname}/{year}/{month}/msg{id}.html
		// pathSegs: ["group", "groupname", "year", "month", "msgID.html"]
		if len(pathSegs) == 5 && pathSegs[0] == "group" && strings.HasPrefix(pathSegs[4], "msg") && strings.HasSuffix(pathSegs[4], ".html") {
			groupName := pathSegs[1]
			yearStr, monthStr := pathSegs[2], pathSegs[3]
			articleNumStr := strings.TrimSuffix(strings.TrimPrefix(pathSegs[4], "msg"), ".html")

			year, errY := strconv.Atoi(yearStr)
			month, errM := strconv.Atoi(monthStr)
			articleNum64, errA := strconv.ParseUint(articleNumStr, 10, 32)
			articleNum := uint32(articleNum64)

			if errY != nil || errM != nil || errA != nil || month < 1 || month > 12 {
				log.Printf("Invalid article path format (canonical): %s. Year: %s, Month: %s, Num: %s", path, yearStr, monthStr, articleNumStr)
				http.Error(w, "Invalid article path format.", http.StatusBadRequest)
				return
			}

			// The rest of the logic for canonical path (fetch by details, date redirect, render)
			article, err := s.db.GetArticleByDetails(groupName, year, month, articleNum)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "not found"){
					log.Printf("Article not found by details: group %s, path %s. Error: %v", groupName, path, err)
					http.Error(w, "Article not found.", http.StatusNotFound)
				} else {
					log.Printf("Error fetching article by details: group %s, path %s. Error: %v", groupName, path, err)
					http.Error(w, "Failed to retrieve article.", http.StatusInternalServerError)
				}
				return
			}

			// Check if URL's year/month matches article's actual received year/month for canonical redirect
			if article.Received.Year() != year || int(article.Received.Month()) != month {
				log.Printf("Date mismatch for article %d in group %s. URL: %d/%d, Article: %d/%d. Redirecting.",
					articleNum, groupName, year, month, article.Received.Year(), article.Received.Month())
				canonicalURL := fmt.Sprintf("/group/%s/%d/%02d/msg%d.html",
					article.GroupName,
					article.Received.Year(),
					article.Received.Month(),
					article.ArticleNum,
				)
				http.Redirect(w, r, canonicalURL, http.StatusFound)
				return
			}

			// Fetch thread messages
			threadMessages, err := s.db.GetThreadMessages(article.ThreadID, article.GroupID, article.ArticleNum)
			if err != nil {
				log.Printf("Error fetching thread messages for article %s (thread %d): %v", article.MessageID, article.ThreadID, err)
			}

			var articleBody string
			var bodyErr error
			if s.nntpClient != nil {
				articleBody, bodyErr = s.nntpClient.FetchArticleBody(article.MessageID, article.GroupName)
				if bodyErr != nil {
					log.Printf("Error fetching article body for %s from NNTP: %v", article.MessageID, bodyErr)
				}
			} else {
				log.Println("NNTP client not initialized, cannot fetch article body.")
				articleBody = "[NNTP client not available to fetch body]"
			}

			data := map[string]interface{}{
				"Article":        article,
				"ThreadMessages": threadMessages,
				"ArticleBody":    articleBody,
			}
			s.renderTemplate(w, r, templates.ArticleView, data)
			return
		}

		// If path structure is not recognized by this handler
		log.Printf("handleShowArticle: Path structure not recognized: %s", path)
		http.NotFound(w, r)
		// Return is implicit here if http.NotFound writes response and this is end of function block
	} // This is the end of the returned http.HandlerFunc
} // This is the end of handleShowArticle method
