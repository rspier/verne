package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/willglynn/nntp"
)

func main() {
	serverAddr := flag.String("server", "", "NNTP server address (e.g., news.example.com:119)")
	groupName := flag.String("group", "", "Newsgroup name (e.g., alt.test)")
	messageCount := flag.Int("n", 10, "Number of messages to fetch")

	flag.Parse()

	if *serverAddr == "" || *groupName == "" {
		fmt.Fprintln(os.Stderr, "Error: Server address and group name are required.")
		flag.Usage() // Prints to os.Stderr by default
		os.Exit(1)
	}

	if *messageCount <= 0 {
		fmt.Fprintln(os.Stderr, "Error: Number of messages to fetch (-n) must be a positive integer.")
		os.Exit(1)
	}

	fmt.Printf("Connecting to server: %s\n", *serverAddr)
	conn, err := nntp.Dial("tcp", *serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to server %s: %v", *serverAddr, err)
	}
	defer conn.Quit() // Ensure Quit is called before exiting

	// Optional: Authenticate if needed, e.g.
	// if err := conn.Authenticate("user", "pass"); err != nil {
	//     log.Fatalf("Authentication failed: %v", err)
	// }

	fmt.Printf("Selecting group: %s\n", *groupName)
	groupInfo, err := conn.Group(*groupName)
	if err != nil {
		log.Fatalf("Failed to select group %s: %v", *groupName, err)
	}

	fmt.Printf("Group selected. Name: %s, Count: %d, Low: %d, High: %d, Status: %s\n",
		groupInfo.Name, groupInfo.Count, groupInfo.Low, groupInfo.High, groupInfo.Status)
	fmt.Printf("Fetching last %d messages...\n\n", *messageCount)

	if groupInfo.Count == 0 {
		fmt.Println("No articles in this group.")
		return
	}

	numToFetch := *messageCount
	if groupInfo.Count < int64(numToFetch) {
		numToFetch = int(groupInfo.Count)
	}

	if numToFetch == 0 { // Possible if groupInfo.Count was 0 and *messageCount > 0
		fmt.Println("No articles to fetch.")
		return
	}

	// Calculate the range of article numbers to fetch
	// Overview(begin, end int64) - begin and end are inclusive.
	endArticleNum := groupInfo.High
	beginArticleNum := groupInfo.High - int64(numToFetch) + 1

	if beginArticleNum < groupInfo.Low {
		beginArticleNum = groupInfo.Low
		// Recalculate numToFetch based on the actual available range if we hit the bottom
		numToFetch = int(endArticleNum - beginArticleNum + 1)
		if numToFetch <=0 {
			fmt.Println("No articles in the calculated range (low water mark too high or group empty).")
			return
		}
	}

	if endArticleNum < beginArticleNum { // Should not happen with above logic if group has articles
	    fmt.Printf("Calculated end article %d is less than begin article %d. No articles to fetch.\n", endArticleNum, beginArticleNum)
	    return
	}


	fmt.Printf("Attempting to fetch overview for articles %d-%d\n", beginArticleNum, endArticleNum)
	overviews, err := conn.Overview(beginArticleNum, endArticleNum)
	if err != nil {
		// Check if the error is because the range is invalid (e.g., group became empty)
		if nntpErr, ok := err.(nntp.Error); ok && nntpErr.Code == 423 { // 423 No articles in specified range
			fmt.Printf("No articles found in range %d-%d (server response: %s)\n", beginArticleNum, endArticleNum, err.Error())
			return
		}
		log.Fatalf("Failed to get overview for articles %d-%d: %v", beginArticleNum, endArticleNum, err)
	}

	if len(overviews) == 0 {
		fmt.Println("No overview data received for the specified range.")
		return
	}

	fmt.Println("\nRecent Messages:")
	// The Overview command usually returns messages sorted by number.
	// We want newest first, so iterate from the end of the slice.
	// The number of items in 'overviews' might be less than 'numToFetch' if some articles are missing in the range.
	// We should display up to 'numToFetch' messages from the received 'overviews', newest first.

	displayCount := 0
	for i := len(overviews) - 1; i >= 0 && displayCount < *messageCount ; i-- {
		item := overviews[i]
		// Format the date. The nntp.MessageOverview.Date is a time.Time object.
		dateStr := "N/A"
		if !item.Date.IsZero() {
			dateStr = item.Date.Format(time.RFC1123Z) // Common format like "Mon, 02 Jan 2006 15:04:05 -0700"
		}

		fmt.Printf("Article: %d\n", item.MessageNumber)
		fmt.Printf("  Subject: %s\n", item.Subject)
		fmt.Printf("  From: %s\n", item.From)
		fmt.Printf("  Date: %s\n", dateStr)
		fmt.Printf("  Message-ID: %s\n", item.MessageId)
		fmt.Println("---")
		displayCount++
	}
}
