package threading

import (
	"nntp-browser-app/backend/pkg/models"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp" // For deep comparison of structs
)

func TestThreadMessages_SimpleParentChild(t *testing.T) {
	now := time.Now()
	msg1 := &models.Message{ID: "msg1", Subject: "Root Message", Date: now}
	msg2 := &models.Message{ID: "msg2", Subject: "Reply 1", References: []string{"<msg1>"}, Date: now.Add(1 * time.Minute)}
	msg3 := &models.Message{ID: "msg3", Subject: "Reply 2", References: []string{"<msg1>", "<msg2>"}, Date: now.Add(2 * time.Minute)}

	messages := []*models.Message{msg1, msg2, msg3}
	ThreadMessages(messages)

	// Check msg1 (root)
	if msg1.Parent != nil {
		t.Errorf("msg1 should be a root message, got parent %s", *msg1.Parent)
	}
	if len(msg1.Children) != 1 || msg1.Children[0].ID != "msg2" {
		t.Errorf("msg1 children expected [msg2], got %v", msg1.Children)
	}

	// Check msg2
	if msg2.Parent == nil || *msg2.Parent != "msg1" {
		t.Errorf("msg2 parent expected msg1, got %v", msg2.Parent)
	}
	if len(msg2.Children) != 1 || msg2.Children[0].ID != "msg3" {
		t.Errorf("msg2 children expected [msg3], got %v", msg2.Children)
	}
    if msg2.PrevMessage != nil {
        t.Errorf("msg2 should not have a prev sibling under msg1's direct children, got %v", *msg2.PrevMessage)
    }
    if msg2.NextMessage != nil {
        t.Errorf("msg2 should not have a next sibling under msg1's direct children, got %v", *msg2.NextMessage)
    }


	// Check msg3
	if msg3.Parent == nil || *msg3.Parent != "msg2" {
		t.Errorf("msg3 parent expected msg2, got %v", msg3.Parent)
	}
	if len(msg3.Children) != 0 {
		t.Errorf("msg3 children expected empty, got %v", msg3.Children)
	}
    if msg3.PrevMessage != nil {
        t.Errorf("msg3 should not have a prev sibling under msg2's direct children, got %v", *msg3.PrevMessage)
    }
}

func TestThreadMessages_MultipleRepliesToRoot(t *testing.T) {
	now := time.Now()
	msg1 := &models.Message{ID: "root", Date: now}
	msg2 := &models.Message{ID: "reply1", References: []string{"<root>"}, Date: now.Add(1 * time.Minute)}
	msg3 := &models.Message{ID: "reply2", References: []string{"<root>"}, Date: now.Add(2 * time.Minute)} // Another reply to root

	messages := []*models.Message{msg1, msg2, msg3} // Order shouldn't matter for parent assignment
	ThreadMessages(messages)

	if len(msg1.Children) != 2 {
		t.Fatalf("root expected 2 children, got %d", len(msg1.Children))
	}
	// Children should be sorted by date: reply1, then reply2
	if msg1.Children[0].ID != "reply1" || msg1.Children[1].ID != "reply2" {
		t.Errorf("root children not sorted correctly by date or incorrect. Got: %s, %s", msg1.Children[0].ID, msg1.Children[1].ID)
	}
    if msg1.Children[0].NextMessage == nil || *msg1.Children[0].NextMessage != "reply2" {
        t.Errorf("reply1 next sibling expected reply2, got %v", msg1.Children[0].NextMessage)
    }
    if msg1.Children[1].PrevMessage == nil || *msg1.Children[1].PrevMessage != "reply1" {
        t.Errorf("reply2 prev sibling expected reply1, got %v", msg1.Children[1].PrevMessage)
    }
}

func TestThreadMessages_NoReferences(t *testing.T) {
	now := time.Now()
	msg1 := &models.Message{ID: "msg1", Date: now}
	msg2 := &models.Message{ID: "msg2", Date: now.Add(1 * time.Minute)}
	messages := []*models.Message{msg1, msg2}
	ThreadMessages(messages)

	if msg1.Parent != nil || len(msg1.Children) != 0 {
		t.Errorf("msg1 should be a standalone root")
	}
	if msg2.Parent != nil || len(msg2.Children) != 0 {
		t.Errorf("msg2 should be a standalone root")
	}
}

func TestThreadMessages_MissingParent(t *testing.T) {
	now := time.Now()
	msg1 := &models.Message{ID: "msg1", References: []string{"<nonexistent>"}, Date: now}
	messages := []*models.Message{msg1}
	ThreadMessages(messages)

	if msg1.Parent != nil {
		t.Errorf("msg1 parent should be nil as <nonexistent> is not in the set, got %v", msg1.Parent)
	}
}


func TestGetThreadRootAndCount(t *testing.T) {
    now := time.Now()
    m1 := &models.Message{ID: "m1", Date: now.Add(1 * time.Second)}
    m2 := &models.Message{ID: "m2", References: []string{"<m1>"}, Date: now.Add(2 * time.Second)}
    m3 := &models.Message{ID: "m3", References: []string{"<m1>", "<m2>"}, Date: now.Add(3 * time.Second)}
    m4 := &models.Message{ID: "m4", References: []string{"<m1>"}, Date: now.Add(4 * time.Second)} // Another child of m1
    m5 := &models.Message{ID: "m5", Date: now.Add(5 * time.Second)} // Separate thread

    messages := []*models.Message{m1, m2, m3, m4, m5}
    ThreadMessages(messages) // Populates Parent/Children pointers

    msgMap := make(map[string]*models.Message)
    for _, msg := range messages {
        msgMap[msg.ID] = msg
    }

    tests := []struct {
        name          string
        startMsgID    string
        expectedRootID string
        expectedCount int
    }{
        {"Root of m1", "m1", "m1", 4},
        {"Root of m2", "m2", "m1", 4},
        {"Root of m3", "m3", "m1", 4},
        {"Root of m4", "m4", "m1", 4},
        {"Root of m5", "m5", "m5", 1},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            root := GetThreadRoot(tt.startMsgID, msgMap)
            if root == nil {
                t.Fatalf("GetThreadRoot(%s) returned nil", tt.startMsgID)
            }
            if root.ID != tt.expectedRootID {
                t.Errorf("GetThreadRoot(%s): expected root %s, got %s", tt.startMsgID, tt.expectedRootID, root.ID)
            }

            count := CountMessagesInThread(root, msgMap)
            if count != tt.expectedCount {
                t.Errorf("CountMessagesInThread for root %s (from %s): expected count %d, got %d", root.ID, tt.startMsgID, tt.expectedCount, count)
            }
        })
    }
}

// Test for self-referencing case to ensure it doesn't loop or misassign.
func TestThreadMessages_SelfReference(t *testing.T) {
	now := time.Now()
	msg1 := &models.Message{ID: "msg1", Subject: "Self Ref", References: []string{"<msg1>"}, Date: now}

	messages := []*models.Message{msg1}
	ThreadMessages(messages)

	if msg1.Parent != nil {
		t.Errorf("msg1 with self-reference should not have a parent set, got %v", *msg1.Parent)
	}
	if len(msg1.Children) != 0 {
		t.Errorf("msg1 with self-reference should have no children, got %v", msg1.Children)
	}
}

// Test for circular references (A -> B, B -> A)
func TestThreadMessages_CircularReference(t *testing.T) {
	now := time.Now()
	msgA := &models.Message{ID: "A", References: []string{"<B>"}, Date: now}
	msgB := &models.Message{ID: "B", References: []string{"<A>"}, Date: now.Add(1 * time.Minute)}

	messages := []*models.Message{msgA, msgB}
	ThreadMessages(messages) // This populates Parent pointers based on last reference

	// msgA's parent is B, msgB's parent is A.
	if msgA.Parent == nil || *msgA.Parent != "B" {
		t.Errorf("msgA parent expected B, got %v", msgA.Parent)
	}
	if msgB.Parent == nil || *msgB.Parent != "A" {
		t.Errorf("msgB parent expected A, got %v", msgB.Parent)
	}

    // Test GetThreadRoot to ensure it handles this cycle
    msgMap := map[string]*models.Message{"A": msgA, "B": msgB} // Corrected map initialization
    rootA := GetThreadRoot("A", msgMap)
    // Depending on implementation, GetThreadRoot might break cycle and return one of them.
    // The current GetThreadRoot detects loops and returns the node where loop detected.
    if rootA.ID != "A" && rootA.ID != "B" { // It should return one of them.
         t.Errorf("GetThreadRoot for A in a cycle should return A or B, got %s", rootA.ID)
    }
}

// Using go-cmp for a more complex structure if needed, but direct assertions are fine for now.
var _ = cmp.Diff
