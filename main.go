package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	// "strconv" // No longer needed

	nntpclient "github.com/kothawoc/go-nntp/client"
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
	nntpClient, err := nntpclient.New("tcp", *serverAddr)
	if err != nil {
		log.Fatalf("Failed to connect to server %s: %v", *serverAddr, err)
	}
	// Note: The library doesn't seem to have an explicit Close() or Quit() method on the client.
	// Connections are typically managed by the underlying net.Conn, which closes on program exit
	// or when garbage collected if no longer referenced.
	// For graceful shutdown, `nntpClient.Command("QUIT", 205)` could be used explicitly if needed,
	// but the library handles command execution and response checking.

	fmt.Printf("Selecting group: %s\n", *groupName)
	groupInfo, err := nntpClient.Group(*groupName)
	if err != nil {
		log.Fatalf("Failed to select group %s: %v", *groupName, err)
	}

	fmt.Printf("Group selected. Count: %d, Low: %d, High: %d\n", groupInfo.Count, groupInfo.Low, groupInfo.High)
	fmt.Printf("Fetching last %d messages...\n\n", *messageCount)

	if groupInfo.Count == 0 {
		fmt.Println("No articles in this group.")
		return
	}

	numToFetch := *messageCount
	if int(groupInfo.Count) < numToFetch {
		numToFetch = int(groupInfo.Count)
	}

	startArticle := groupInfo.High - int64(numToFetch) + 1
	if startArticle < groupInfo.Low {
		startArticle = groupInfo.Low
		numToFetch = int(groupInfo.High - groupInfo.Low + 1) // Adjust numToFetch if we hit the bottom
	}

	var articleNumbersToFetch []int
	if groupInfo.High > 0 { // Ensure High is a valid article number
		for i := 0; i < numToFetch; i++ {
			articleNum := groupInfo.High - int64(i)
			if articleNum < groupInfo.Low { // Stop if we go below the lowest article number
				break
			}
			articleNumbersToFetch = append(articleNumbersToFetch, int(articleNum))
		}
	} else {
		fmt.Println("Group high water mark is 0, cannot fetch articles by number range from high.")
        // Potentially try NEWNEWS if OVER by range fails or is not applicable.
        // However, the task is to get the *most recent*, which implies working downwards from High.
        // If High is 0, it implies an empty or problematic group state for this approach.
		return
	}


	if len(articleNumbersToFetch) == 0 {
		fmt.Println("No articles to fetch in the calculated range.")
		return
	}

	// The nntpclient.Over() command is used to fetch article overview information for a range.
	// We've calculated the article numbers for the most recent 'numToFetch' articles.
	// fetchStartNum is the oldest (smallest number) and fetchEndNum is the newest (largest number) in that set.
	fetchStartNum := articleNumbersToFetch[len(articleNumbersToFetch)-1] // smallest number in our target list (oldest)
	fetchEndNum := articleNumbersToFetch[0]                             // largest number in our target list (newest)

	fmt.Printf("Attempting to fetch overview for articles %d-%d\n", fetchStartNum, fetchEndNum)
	overItems, err := nntpClient.Over(fetchStartNum, fetchEndNum) // Fetches articles in the range [fetchStartNum, fetchEndNum]
	if err != nil {
		log.Fatalf("Failed to get overview for articles %d-%d: %v", fetchStartNum, fetchEndNum, err)
	}

	// Sort OverItems by article number descending if necessary, though OVER usually returns them sorted.
	// The library might already sort them or return as is. We need the newest first.
	// The request is for the "most recent 10 messages". So we need to display them from newest to oldest.
	// `groupInfo.High` is the newest. So items should be processed from highest number to lowest.
	// The `overItems` from `Over(start, end)` should be in ascending order of article number.
	// We need to reverse this for display or pick the correct slice.

	// Filter down to the actual number of messages requested, from the newest ones.
	actualMessages := []nntpclient.OverItem{}
	for i := len(overItems) - 1; i >= 0 && len(actualMessages) < *messageCount; i-- {
		actualMessages = append(actualMessages, overItems[i])
	}


	fmt.Println("\nRecent Messages:")
	for _, item := range actualMessages {
		// The OverItem struct has Subject, From, Date, MessageId
		// Date format from NNTP OVER command can vary. Let's see what we get.
		// Standard mail date format is "Date: Mon, 2 Jan 2006 15:04:05 -0700 (MST)"
		// The `OverItem.Date` field should contain this.
		fmt.Printf("Article: %s\n", item.Number) // item.Number is a string
		fmt.Printf("  Subject: %s\n", item.Subject)
		fmt.Printf("  From: %s\n", item.From)
		fmt.Printf("  Date: %s\n", item.Date)
		fmt.Printf("  Message-ID: %s\n", item.MessageId)
		fmt.Println("---")
	}

	// Attempt to gracefully quit
	_, _, err = nntpClient.Command("QUIT", 205) // 205 Service closing transmission channel
	if err != nil {
		// Log non-fatal error as we are exiting anyway
		log.Printf("Error sending QUIT command: %v (this might be expected if server closes connection first)", err)
	}
}
