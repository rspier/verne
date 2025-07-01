package threading

import (
	"log"
	"nntp-browser-app/backend/pkg/models"
	"sort"
	"strings"
)

// ThreadMessages takes a flat list of messages (typically MessageOverview or full Message structs
// that include at least ID and References) and attempts to arrange them into threads.
// This is a simplified threading model.
// It populates Parent and Children relationships IN-PLACE if full Message objects are passed.
// For MessageOverview, it might just calculate MessageCountInThread.
// This version will assume we are working with full models.Message for now.
func ThreadMessages(messages []*models.Message) {
	if len(messages) == 0 {
		return
	}

	msgMap := make(map[string]*models.Message) // Map Message-ID to Message pointer
	for _, msg := range messages {
		msgMap[msg.ID] = msg
		msg.Children = []*models.Message{} // Initialize children slice
	}

	// Build parent-child relationships
	for _, msg := range messages {
		if len(msg.References) > 0 {
			// The last reference is usually the direct parent
			parentID := msg.References[len(msg.References)-1]
			// References might include angle brackets, msg.ID does not.
			parentID = strings.Trim(parentID, "<>")

			if parent, exists := msgMap[parentID]; exists {
				// Check for self-reference to prevent cycles with bad data
				if parent.ID == msg.ID {
					log.Printf("Warning: Message %s references itself. Skipping parent assignment.", msg.ID)
					continue
				}
				msg.Parent = &parent.ID
				parent.Children = append(parent.Children, msg)
			}
		}
	}

	// Sort children by date for each message
	for _, msg := range messages {
		if len(msg.Children) > 0 {
			sort.Slice(msg.Children, func(i, j int) bool {
				return msg.Children[i].Date.Before(msg.Children[j].Date)
			})
		}
	}

	// Identify root messages (those without a known parent in this set)
	// and link next/prev messages within each thread.
	var rootMessages []*models.Message
	for _, msg := range messages {
		if msg.Parent == nil {
			rootMessages = append(rootMessages, msg)
		}
	}

	// Sort root messages by date
	sort.Slice(rootMessages, func(i, j int) bool {
		return rootMessages[i].Date.Before(rootMessages[j].Date)
	})

	// For each root, traverse its thread to link next/prev
	for _, root := range rootMessages {
		linkSiblingsInThread(root)
	}

	log.Printf("Threading complete. Found %d root messages.", len(rootMessages))
}

// linkSiblingsInThread recursively traverses children and sets NextMessage/PrevMessage
func linkSiblingsInThread(parent *models.Message) {
	for i, child := range parent.Children {
		if i > 0 {
			prevSiblingID := parent.Children[i-1].ID
			child.PrevMessage = &prevSiblingID
		}
		if i < len(parent.Children)-1 {
			nextSiblingID := parent.Children[i+1].ID
			child.NextMessage = &nextSiblingID
		}
		linkSiblingsInThread(child) // Recurse for grandchildren
	}
}


// CalculateThreadCounts updates MessageOverview with the count of messages in their respective threads.
// This is a separate function as MessageOverview doesn't store full thread structure.
// Assumes `messages` are `models.MessageOverview` and `allMessages` are `models.Message` (fully threaded).
// This function is more complex to implement efficiently without full message objects.
// A simpler approach for MessageOverview:
// 1. Group all available full messages by Message-ID.
// 2. For each message, find its root parent using the References.
// 3. Count all messages that share the same root.
// This is still complex. For now, GetMessagesHandler will fetch full messages, thread them,
// then create MessageOverview objects, populating MessageCountInThread.

func GetThreadRoot(msgID string, msgMap map[string]*models.Message) *models.Message {
    msg, ok := msgMap[msgID]
    if !ok || msg == nil {
        return nil
    }
    // Keep traversing up via Parent pointer until a message with no parent is found
    // or we detect a loop (though our simple threading above tries to avoid direct self-loops)
    visited := make(map[string]bool)
    current := msg
    for current.Parent != nil {
        if visited[*current.Parent] {
            log.Printf("Loop detected while finding root for message %s at parent %s", msgID, *current.Parent)
            return current // Return current as a pseudo-root to break loop
        }
        visited[*current.Parent] = true

        parentMsg, parentExists := msgMap[*current.Parent]
        if !parentExists {
            break // Parent not in the current set, so this is a root relative to the set
        }
        current = parentMsg
    }
    return current
}

func CountMessagesInThread(rootMessage *models.Message, msgMap map[string]*models.Message) int {
    if rootMessage == nil {
        return 0
    }
    count := 0
    // Use BFS or DFS to count all descendants of the rootMessage
    queue := []*models.Message{rootMessage}
    visited := make(map[string]bool)

    for len(queue) > 0 {
        current := queue[0]
        queue = queue[1:]

        if visited[current.ID] {
            continue
        }
        visited[current.ID] = true
        count++

        for _, child := range current.Children {
            if child != nil && !visited[child.ID] { // Ensure child is in msgMap if using map directly
                queue = append(queue, child)
            }
        }
    }
    return count
}
