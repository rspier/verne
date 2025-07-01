package nntp

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/user/colobus/internal/config"
	"github.com/user/colobus/internal/database"
)

// Session represents a single NNTP client connection.
type Session struct {
	conn           net.Conn
	reader         *bufio.Reader
	writer         *bufio.Writer
	config         *config.Config
	db             *database.DB
	remoteAddr     string
	id             string // Unique session ID for logging, e.g., remoteAddr + timestamp
	serverVersion  string
	groupNameToNum map[string]int // Effective mapping of group name to numeric ID
	groupNumToName map[int]string   // Effective mapping of group numeric ID to name

	// Session state
	selectedGroup     *config.GroupConfig // Full config of the currently selected group
	currentArticleNum int64               // Article number in selectedGroup
	modeReader        bool                // Default NNTP mode
}

// NewSession creates a new NNTP session.
func NewSession(
	conn net.Conn,
	cfg *config.Config,
	db *database.DB,
	groupNameToNum map[string]int,
	groupNumToName map[int]string,
	remoteAddr string,
) *Session {
	// serverVersion can be more dynamic, e.g., include git hash if available
	// For now, using a fixed version string.
	// The Perl script dynamically gets version: $Colobus::VERSION = "3.0"; if (-e ".git") { $Colobus::VERSION .= " (`git describe`)"; }
	serverVersion := "Colobus-Go/0.1" // Placeholder

	// Create a simple session ID
	sessionID := fmt.Sprintf("%s-%d", remoteAddr, time.Now().UnixNano())

	return &Session{
		conn:           conn,
		reader:         bufio.NewReader(conn),
		writer:         bufio.NewWriter(conn),
		config:         cfg,
		db:             db,
		remoteAddr:     remoteAddr, // Already just the address string
		id:             sessionID,
		serverVersion:  serverVersion,
		groupNameToNum: groupNameToNum,
		groupNumToName: groupNumToName,
		modeReader:     true, // Default mode
	}
}

// Close closes the session connection.
func (s *Session) Close() {
	log.Printf("[%s] Closing connection.", s.id)
	s.conn.Close()
}

// respond sends a formatted response to the client.
func (s *Session) respond(code int, message string) error {
	log.Printf("[%s] RES: %d %s", s.id, code, message)
	_, err := fmt.Fprintf(s.writer, "%d %s\r\n", code, message)
	if err != nil {
		log.Printf("[%s] Error writing response: %v", s.id, err)
		return err
	}
	return s.writer.Flush()
}

// sendTextLines sends a list of strings as separate lines, terminated by ".\r\n".
// Used for commands like HELP, LIST. Lines are sent as-is (no dot-stuffing).
func (s *Session) sendTextLines(lines []string) error {
	for _, line := range lines {
		// NNTP spec for text responses (like LIST, HELP) does NOT require dot-stuffing for lines starting with "."
		if _, err := s.writer.WriteString(line + "\r\n"); err != nil {
			return err
		}
	}
	if _, err := s.writer.WriteString(".\r\n"); err != nil {
		return err
	}
	return s.writer.Flush()
}

// sendArticleData sends article content (headers and/or body).
// Handles dot-stuffing for lines beginning with a period.
// `includeHeaders` and `includeBody` control what parts are sent.
func (s *Session) sendArticleData(articlePath string, includeHeaders bool, includeBody bool, additionalHeaders []string) error {
	file, err := os.Open(articlePath)
	if err != nil {
		return fmt.Errorf("opening article file %s: %w", articlePath, err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	headersDone := false

	if includeHeaders {
		for _, hline := range additionalHeaders {
			// These are already formatted, just write them
			if _, err := s.writer.WriteString(hline + "\r\n"); err != nil {
				return err
			}
		}
	}

	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n") // Normalize, will re-add \r\n

		if !headersDone {
			if line == "" { // Empty line signifies end of headers
				headersDone = true
				if includeHeaders && includeBody { // Only print CRLF if both are to be printed
					if _, errw := s.writer.WriteString("\r\n"); errw != nil {
						return errw
					}
				}
				if !includeBody { // If only headers, we are done after this (and the final dot)
					goto endLoop
				}
				if err == io.EOF { goto endLoop }
				continue
			}
			if includeHeaders {
				// Basic dot-stuffing for headers, though usually not an issue for valid headers
				if strings.HasPrefix(line, ".") {
					line = "." + line
				}
				if _, errw := s.writer.WriteString(line + "\r\n"); errw != nil {
					return errw
				}
			}
		} else { // Body processing
			if includeBody {
				// Dot-stuffing for body lines
				if strings.HasPrefix(line, ".") {
					line = "." + line
				}
				if _, errw := s.writer.WriteString(line + "\r\n"); errw != nil {
					return errw
				}
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading article line: %w", err)
		}
	}

endLoop:
	if _, err := s.writer.WriteString(".\r\n"); err != nil {
		return err
	}
	return s.writer.Flush()
}


// Serve handles the NNTP command loop for the session.
func (s *Session) Serve() {
	serverGreeting := fmt.Sprintf("%s - %s ready - (posting ok)", s.config.ServerName, s.serverVersion)
	if err := s.respond(200, serverGreeting); err != nil {
		log.Printf("[%s] Error sending greeting: %v", s.id, err)
		return
	}

	for {
		if s.config.Timeout > 0 {
			s.conn.SetReadDeadline(time.Now().Add(time.Duration(s.config.Timeout) * time.Second))
		} else {
			s.conn.SetReadDeadline(time.Time{}) // No deadline
		}

		line, err := s.reader.ReadString('\n')
		if err != nil {
			// Handle different types of errors
			if opError, ok := err.(*net.OpError); ok && opError.Timeout() {
				log.Printf("[%s] Read timeout.", s.id)
				s.respond(400, "Idle timeout, closing connection.") // Or a more specific NNTP code if available
			} else if err == io.EOF {
				log.Printf("[%s] Connection closed by client (EOF).", s.id)
			} else {
				log.Printf("[%s] Error reading command: %v", s.id, err)
			}
			return // End session
		}

		line = strings.TrimRight(line, "\r\n")
		log.Printf("[%s] CMD: %s", s.id, line)

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue // Empty line
		}

		command := strings.ToLower(parts[0])
		args := parts[1:]

		if _, disallowed := s.config.Disallow[command]; disallowed {
			s.respond(502, fmt.Sprintf("Command %s disabled by administrator.", command))
			continue
		}

		// Command dispatching
		// This structure mimics the Perl script's dispatch approach.
		switch command {
		case "quit":
			s.handleQuit(args)
			return // End session
		case "help":
			s.handleHelp(args)
		case "date":
			s.handleDate(args)
		case "mode":
			s.handleMode(args)
		case "group":
			s.handleGroup(args)
		case "list":
			s.handleList(args)
		case "article", "head", "body", "stat":
			s.handleArticleFamily(command, args)
		case "next":
			s.handleNext(args)
		case "last":
			s.handleLast(args)
		case "post":
			s.handlePost(args)
		case "ihave":
			s.handleIhave(args)
		// case "newgroups":
		// 	s.handleNewGroups(args)
		// case "newnews":
		// 	s.handleNewNews(args)
		case "listgroup":
			s.handleListGroup(args)
		case "slave":
			s.handleSlave(args)
		// case "xgtitle":
		// 	s.handleXGTitle(args)
		case "xhdr", "xpat", "xrover": // XPAT and XROVER are handled by XHDR in Perl
			s.handleXhdr(command, args)
		case "xover":
			s.handleXover(args)
		case "xpath":
			s.handleXPath(args)
		case "xversion":
			s.handleXVersion(args)
		default:
			s.respond(500, fmt.Sprintf("Command '%s' not recognized or not implemented.", command))
		}
	}
}

// --- Utility methods ---

func (s *Session) getArticlePath(groupCfg *config.GroupConfig, articleNum int64) (string, error) {
	if groupCfg == nil || groupCfg.Path == "" {
		return "", fmt.Errorf("group path not configured")
	}
	// Perl: sprintf("%s/archive/%d/%02d", $groups{$group}->{path}, int $artno / 100, $artno % 100);
	// This implies a specific directory structure.
	// Ensure base path exists or handle error.
	// For safety, clean the path components.
	archiveDir := filepath.Clean(fmt.Sprintf("%d", articleNum/100))
	articleFile := filepath.Clean(fmt.Sprintf("%02d", articleNum%100))
	if strings.Contains(archiveDir, "..") || strings.Contains(articleFile, "..") {
		return "", fmt.Errorf("invalid article number components for path calculation")
	}
	return filepath.Join(groupCfg.Path, "archive", archiveDir, articleFile), nil
}

// md5Hex computes the MD5 hash of a string and returns its hex representation.
func md5Hex(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}

// parseMessageID extracts the content from <message-id>.
func parseMessageID(messageIDHeader string) (string, bool) {
	if strings.HasPrefix(messageIDHeader, "<") && strings.HasSuffix(messageIDHeader, ">") {
		return messageIDHeader[1 : len(messageIDHeader)-1], true
	}
	return "", false
}

// --- Command Handlers (alphabetical for easier navigation later) ---

func (s *Session) handleArticleFamily(command string, args []string) {
	var targetArg string
	if len(args) > 0 {
		targetArg = args[0]
	}

	var articleNum int64
	var msgID string
	var groupCfgForMsgID *config.GroupConfig // Group associated with the message-id, if provided

	if targetArg == "" { // Use current article in selected group
		if s.selectedGroup == nil {
			s.respond(412, "No newsgroup selected")
			return
		}
		if s.currentArticleNum == 0 { // Or some other indicator of "no current article"
			s.respond(420, "No current article has been selected")
			return
		}
		articleNum = s.currentArticleNum
	} else if strings.HasPrefix(targetArg, "<") && strings.HasSuffix(targetArg, ">") { // Message-ID specified
		msgID = targetArg
		parsedMsgID, ok := parseMessageID(msgID)
		if !ok {
			s.respond(501, "Invalid message-ID format")
			return
		}
		// Perl: ($ggg,$which) = get_group_and_article($id);
		// This involves a DB lookup on md5_hex(parsedMsgID)
		hashedMsgID := md5Hex(parsedMsgID)
		dbGroupID, dbArticleNum, found, err := s.db.GetArticleGroupAndNumByHashedMsgID(hashedMsgID)
		if err != nil {
			log.Printf("[%s] DB error looking up msgid %s (hashed %s): %v", s.id, parsedMsgID, hashedMsgID, err)
			s.respond(430, "No such article found (server error)") // Or 430
			return
		}
		if !found {
			// Perl also tries to parse msgid like "group-articlenumber@servername"
			// This is a fallback if DB lookup fails.
			// serverNamePart := "@" + s.config.ServerName
			// if strings.HasSuffix(parsedMsgID, serverNamePart) {
			//    trimmed := strings.TrimSuffix(parsedMsgID, serverNamePart)
			//    parts := strings.Split(trimmed, "-")
			//    if len(parts) == 2 {
			//        // potentialGroupName := parts[0]
			//        // potentialArticleNumStr := parts[1]
			//        // ... further validation ...
			//    }
			// }
			s.respond(430, "No such article found")
			return
		}
		groupNameForMsgID, ok := s.groupNumToName[dbGroupID]
		if !ok {
			log.Printf("[%s] MsgID %s resolved to unknown group ID %d", s.id, parsedMsgID, dbGroupID)
			s.respond(430, "No such article found (group configuration error)")
			return
		}
		groupCfgForMsgID, ok = s.config.Groups[groupNameForMsgID]
		if !ok {
			log.Printf("[%s] MsgID %s resolved to group name %s with no config", s.id, parsedMsgID, groupNameForMsgID)
			s.respond(430, "No such article found (group configuration error)")
			return
		}
		articleNum = dbArticleNum
	} else { // Article number specified
		if s.selectedGroup == nil {
			s.respond(412, "No newsgroup selected (and argument was not a message-ID)")
			return
		}
		var err error
		articleNum, err = strconv.ParseInt(targetArg, 10, 64)
		if err != nil {
			s.respond(423, fmt.Sprintf("Invalid article number: %s", targetArg))
			return
		}
	}

	// Determine which group's context to use
	currentGroupContext := s.selectedGroup
	if groupCfgForMsgID != nil {
		currentGroupContext = groupCfgForMsgID
	}

	if currentGroupContext == nil { // Should have been caught by earlier checks
		s.respond(412, "No newsgroup context for article retrieval")
		return
	}

	// Fetch overview header from DB
	// Perl: my ($xover) = get_article_xover($ggg||$group, $which)
	dbArtHeader, err := s.db.GetArticleHeaderByNum(currentGroupContext.Num, articleNum)
	if err != nil {
		log.Printf("[%s] DB error fetching article header for group %s (%d), article %d: %v", s.id, currentGroupContext.Name, currentGroupContext.Num, articleNum, err)
		s.respond(423, "No such article number in this group (database error)")
		return
	}
	if dbArtHeader == nil { // Not found
		s.respond(423, "No such article number in this group")
		return
	}

	// Use the Message-ID from the DB header if available, otherwise construct one.
	// Perl: $id ||= get_article_message_id_from_xover($group,$xover);
	// $xover->{'Message-ID'} comes from h_messageid
	finalMsgIDStr := ""
	if dbArtHeader.MessageID.Valid && dbArtHeader.MessageID.String != "" {
		finalMsgIDStr = dbArtHeader.MessageID.String // Should already be <...>
	} else {
		// Construct one like "groupname-articlenum@servername"
		finalMsgIDStr = fmt.Sprintf("<%s-%d@%s>", currentGroupContext.Name, articleNum, s.config.ServerName)
	}


	// Determine what to send based on command
	var respCode int
	var respMsg string
	includeHeaders := false
	includeBody := false

	switch command {
	case "stat":
		respCode = 223
		respMsg = fmt.Sprintf("%d %s article retrieved - request text separately", articleNum, finalMsgIDStr)
		// Update current article pointer if not a specific msg-id lookup outside current group
		if groupCfgForMsgID == nil || groupCfgForMsgID == s.selectedGroup {
			s.currentArticleNum = articleNum
		}
	case "head":
		respCode = 221
		respMsg = fmt.Sprintf("%d %s article retrieved - head follows", articleNum, finalMsgIDStr)
		includeHeaders = true
	case "body":
		respCode = 222
		respMsg = fmt.Sprintf("%d %s article retrieved - body follows", articleNum, finalMsgIDStr)
		includeBody = true
	case "article":
		respCode = 220
		respMsg = fmt.Sprintf("%d %s article retrieved - head and body follow", articleNum, finalMsgIDStr)
		includeHeaders = true
		includeBody = true
	default: // Should not happen
		s.respond(500, "Internal server error in article handling")
		return
	}

	if err := s.respond(respCode, respMsg); err != nil {
		return // Error writing response, session likely broken
	}

	if command == "stat" {
		return // Nothing more to send for STAT
	}

	// Prepare additional headers (Path, Xref, Newsgroups, Followup-To)
	// These are synthesized, not necessarily from the raw article file.
	additionalHeaders := []string{}
	if includeHeaders {
		// Newsgroups: from Xref (comma separated)
		// Xref: full "Servername group1:artid1 group2:artid2 ..."
		// Path: Servername (or more complex if it's a feed)

		// Construct Xref and Newsgroups
		// Perl: my $res = $dbh->selectall_arrayref(qq{SELECT group_id,id FROM articles WHERE msgid = ?}, undef, md5_hex($msgid));
		// $xover->{'xref'} = join ' ', map { $groupsbyid{$_->[0]}.":".$_->[1] } @$res if $res;
		// $xover->{'xref'} ||= $group . ":" . $xover->{'id'};
		// ($xover->{'newsgroups'} = $xover->{'xref'}) =~ s/(:\d+)//g; $xover->{'newsgroups'} =~ s/\s+/,/g;

		var xrefParts []string
		var newsgroupsParts []string

		// Use HashedMsgID from the dbArtHeader for Xref lookup
		if dbArtHeader.HashedMsgID != "" {
			xrefs, err := s.db.GetArticleXrefsByHashedMsgID(dbArtHeader.HashedMsgID)
			if err != nil {
				log.Printf("[%s] DB error getting xrefs for %s: %v", s.id, dbArtHeader.HashedMsgID, err)
				// Continue without full Xref, or with minimal
			}
			if len(xrefs) > 0 {
				for gID, aID := range xrefs {
					gName, ok := s.groupNumToName[gID]
					if ok {
						xrefParts = append(xrefParts, fmt.Sprintf("%s:%d", gName, aID))
						newsgroupsParts = append(newsgroupsParts, gName)
					}
				}
			}
		}
		if len(xrefParts) == 0 { // Fallback if no xrefs found or HashedMsgID was empty
			xrefParts = append(xrefParts, fmt.Sprintf("%s:%d", currentGroupContext.Name, articleNum))
			newsgroupsParts = append(newsgroupsParts, currentGroupContext.Name)
		}

		additionalHeaders = append(additionalHeaders, "Path: "+s.config.ServerName) // Simplified path
		additionalHeaders = append(additionalHeaders, "Xref: "+s.config.ServerName+" "+strings.Join(xrefParts, " "))
		additionalHeaders = append(additionalHeaders, "Newsgroups: "+strings.Join(newsgroupsParts, ","))

		if currentGroupContext.Followup != "" {
			additionalHeaders = append(additionalHeaders, "Followup-To: "+currentGroupContext.Followup)
		}
		// The Perl script adds Approved: news@$config{servername} if moderated. This is usually added by humans or moderation software.
		// It also adds From and Date if missing from original, using DB values or stat() of file.
		// For now, we assume the raw article file has these.
	}

	articleFilePath, err := s.getArticlePath(currentGroupContext, articleNum)
	if err != nil {
		log.Printf("[%s] Error getting article path for group %s article %d: %v", s.id, currentGroupContext.Name, articleNum, err)
		// We've already sent the initial response code. This is tricky.
		// We could try to send an error, but the client expects article data or ".\r\n"
		// For now, if file path fails, we just send the ".\r\n"
		if _, wErr := s.writer.WriteString(".\r\n"); wErr != nil {
			log.Printf("[%s] Error writing final dot after article path error: %v", s.id, wErr)
		}
		s.writer.Flush()
		return
	}

	if err := s.sendArticleData(articleFilePath, includeHeaders, includeBody, additionalHeaders); err != nil {
		log.Printf("[%s] Error sending article data for group %s article %d from %s: %v", s.id, currentGroupContext.Name, articleNum, articleFilePath, err)
		// Session might be broken, error already logged by sendArticleData if write fails.
	}
}


func (s *Session) handleDate(args []string) {
	s.respond(111, time.Now().UTC().Format("20060102150405")) // YYYYMMDDHHMMSS
}

func (s *Session) handleGroup(args []string) {
	if len(args) < 1 {
		s.respond(501, "GROUP command requires a group name argument")
		return
	}
	groupName := strings.ToLower(args[0])

	groupCfg, ok := s.config.Groups[groupName]
	if !ok || groupCfg.Num == 0 { // Num == 0 implies it wasn't properly synced/found in DB
		s.respond(411, fmt.Sprintf("No such newsgroup %s", groupName))
		return
	}

	// Get min/max/count from DB
	// Perl: my ($min, $max) = get_group_minmax($ggg)
	//       my $count = $max - $min + 1;
	minArticle, maxArticle, count, err := s.db.GetGroupMinMaxArticleIDs(groupCfg.Num)
	if err != nil {
		log.Printf("[%s] DB error getting min/max for group %s (ID %d): %v", s.id, groupName, groupCfg.Num, err)
		s.respond(411, fmt.Sprintf("Error accessing group %s", groupName)) // Internal error
		return
	}

	// If count is 0, NNTP response typically has first = 1, last = 0 for an empty group.
	// Or first = 0, last = -1. RFC3977 suggests estimated number of articles, first, last.
	// Let's adjust minArticle if count is 0 for a more standard response.
	// If maxArticle is 0 (or less than minArticle for some reason), it means no articles or empty group.
	// Common practice: if empty, count=0, min=1, max=0.
	if count == 0 {
		minArticle = 1 // Convention for empty group's first article number
		maxArticle = 0 // Convention for empty group's last article number
	}


	s.selectedGroup = groupCfg
	s.currentArticleNum = minArticle // Set current article to the first in the group

	// 211 {estimated_article_count} {first_article_num} {last_article_num} {group_name}
	s.respond(211, fmt.Sprintf("%d %d %d %s", count, minArticle, maxArticle, groupName))
}

func (s *Session) handleHelp(args []string) {
	// This list should be dynamically generated based on available and allowed commands.
	// For now, a static list similar to the Perl script's implied commands.
	helpText := []string{
		"ARTICLE [<messageid>|artnum]",
		"BODY [<messageid>|artnum]",
		"DATE",
		"GROUP newsgroup",
		"HEAD [<messageid>|artnum]",
		"HELP",
		"IHAVE <messageid>",
		"LAST",
		"LIST [ACTIVE|NEWSGROUPS|OVERVIEW.FMT|SUBSCRIPTIONS|ACTIVE.TIMES|DISTRIBUTIONS|DISTRIB.PATS]",
		"LISTGROUP [newsgroup]",
		"MODE READER",
		// "NEWGROUPS yymmdd hhmmss [GMT] [<distributions>]",
		// "NEWNEWS newsgroups yymmdd hhmmss [GMT] [<distributions>]",
		"NEXT",
		"POST",
		"QUIT",
		"SLAVE",
		"STAT [<messageid>|artnum]",
		// "XGTITLE [wildmat]", // Often same as LIST NEWSGROUPS description
		"XHDR header [range|<messageid>]",
		"XOVER [range]",
		// "XPAT header pattern [range|<messageid>]", // Via XHDR
		"XPATH <messageid>",
		// "XROVER [range]", // Via XHDR for References
		"XVERSION",
	}
	s.respond(100, "Help text follows")
	s.sendTextLines(helpText)
}

func (s *Session) handleIhave(args []string) {
    if len(args) < 1 {
        s.respond(480, "IHAVE requires a message-ID argument") // 480 Auth required, 435 not wanted, 501 syntax
        return
    }
    msgIDStr := args[0]
    parsedMsgID, ok := parseMessageID(msgIDStr)
    if !ok {
        s.respond(501, "Invalid message-ID format for IHAVE")
        return
    }

    hashedMsgID := md5Hex(parsedMsgID)
    exists, err := s.db.CheckHashedMessageIDExists(hashedMsgID)
    if err != nil {
        log.Printf("[%s] DB error checking msgid %s for IHAVE: %v", s.id, parsedMsgID, err)
        s.respond(436, "Transfer failed - try again later (server error)")
        return
    }

    if exists {
        s.respond(435, "Article not wanted - do not send it (already have it)")
        return
    }

    s.respond(335, "Send article to be transferred. End with <CR-LF>.<CR-LF>")
    // Now call a method similar to handlePost, but with different success/failure codes.
    s.receiveArticle(true, parsedMsgID, hashedMsgID)
}


func (s *Session) handleLast(args []string) {
	if s.selectedGroup == nil {
		s.respond(412, "No newsgroup selected")
		return
	}
	if s.currentArticleNum == 0 { // Assuming 0 means no current article or beginning
		s.respond(420, "No current article selected")
		return
	}

	minArticle, _, _, err := s.db.GetGroupMinMaxArticleIDs(s.selectedGroup.Num)
	if err != nil {
		log.Printf("[%s] DB error in LAST for group %s: %v", s.id, s.selectedGroup.Name, err)
		s.respond(500, "Server error processing LAST")
		return
	}

	if s.currentArticleNum <= minArticle {
		s.respond(422, "No previous article in this group")
		return
	}

	s.currentArticleNum--
	// Fetch actual message-id for the response
	header, err := s.db.GetArticleHeaderByNum(s.selectedGroup.Num, s.currentArticleNum)
	if err != nil || header == nil {
		log.Printf("[%s] DB error fetching header for LAST article %d in %s: %v", s.id, s.currentArticleNum, s.selectedGroup.Name, err)
		// This case is tricky: we've moved the pointer, but can't confirm the article.
		// Maybe respond with currentArticleNum and a generic/constructed message-id.
		s.respond(422, "No previous article in this group (or error fetching it)")
		// It might be better to check if currentArticleNum-1 exists before decrementing.
		return
	}

	msgIDStr := ""
	if header.MessageID.Valid && header.MessageID.String != "" {
		msgIDStr = header.MessageID.String
	} else {
		msgIDStr = fmt.Sprintf("<%s-%d@%s>", s.selectedGroup.Name, s.currentArticleNum, s.config.ServerName)
	}
	s.respond(223, fmt.Sprintf("%d %s article retrieved - request text separately", s.currentArticleNum, msgIDStr))
}

func (s *Session) handleList(args []string) {
	listType := "active" // Default if no argument
	var wildmatPattern string

	if len(args) > 0 {
		listType = strings.ToLower(args[0])
		if len(args) > 1 {
			wildmatPattern = args[1] // For commands like LIST ACTIVE [wildmat]
		}
	}

	// TODO: Implement wildmat matching if wildmatPattern is provided.
	// For now, ignore wildmatPattern.
	_ = wildmatPattern // Placeholder

	switch listType {
	case "active":
		s.respond(215, "List of active newsgroups follows")
		var lines []string
		for name, groupCfg := range s.config.Groups {
			if groupCfg.Hidden && wildmatPattern == "" { // Only hide if no pattern given (Perl logic)
				continue
			}
			if groupCfg.Num == 0 { // Skip groups not properly synced/numbered
				continue
			}
			min, max, _, err := s.db.GetGroupMinMaxArticleIDs(groupCfg.Num)
			if err != nil {
				log.Printf("[%s] LIST ACTIVE: DB error for group %s: %v", s.id, name, err)
				continue // Skip this group on error
			}
			// Posting status: 'y' (yes), 'n' (no), 'm' (moderated)
			// Perl: my $act = get_group_active($_); which checks $groups{$_}->{mail} and $groups{$_}->{moderated}
			var status = "y" // Default
			if groupCfg.Mail == "" { // Assuming no mail means no direct posting
				status = "n"
			}
			if groupCfg.Moderated {
				status = "m"
			}
			// RFC3977: max first status name
			// Ensure max is not less than min if articles exist. If empty, max might be 0, min 1.
			if max < min && count <=0 { // Empty group or no articles
				max = 0 // Convention for last article in empty group
				min = 1 // Convention for first article in empty group
			}

			// Perl: printf "%s %010d %010d %s\r\n", $_, $max, $min, $act if $max && $act;
			// The condition `if $max && $act` means if $max is non-zero (group has articles) and $act is not empty/false.
			// Our `status` is always y, n, or m. So we mainly care if max > 0 (or count > 0 after adjustment)
			if max > 0 || (max == 0 && min == 1) { // If group has articles or is conventionally empty
			    lines = append(lines, fmt.Sprintf("%s %010d %010d %s", name, max, min, status))
			}
		}
		s.sendTextLines(lines)

	case "newsgroups":
		s.respond(215, "List of newsgroup descriptions follows")
		var lines []string
		for name, groupCfg := range s.config.Groups {
			if groupCfg.Hidden && wildmatPattern == "" {
				continue
			}
			// Perl: my $desc = get_group_description($_) or next; print "$_ $desc\r\n";
			if groupCfg.Desc != "" {
				lines = append(lines, fmt.Sprintf("%s %s", name, groupCfg.Desc))
			} else {
				lines = append(lines, name) // Group name if no description
			}
		}
		s.sendTextLines(lines)

	case "overview.fmt":
		s.respond(215, "Order of fields in overview database")
		var lines []string
		// Perl: foreach (@overview) { print "$_:\r\n"; } print "Xref:full\r\n";
		// @overview = qw(Subject From Date Message-ID References Bytes Lines);
		for _, field := range s.config.OverviewFields {
			lines = append(lines, field+":") // Original Perl script sends "Header:\r\n"
		}
		lines = append(lines, "Xref:full") // Xref is special
		s.sendTextLines(lines)

	case "subscriptions": // LIST SUBSCRIPTIONS
		s.respond(215, "List of suggested subscriptions follows")
		var lines []string
		for name, groupCfg := range s.config.Groups {
			if groupCfg.Recommend {
				lines = append(lines, name)
			}
		}
		s.sendTextLines(lines)

	// TODO: Implement other LIST subcommands: active.times, distributions, distrib.pats
	// active.times: needs creation time. Perl uses stat of first article. DB might store this.
	// distributions, distrib.pats: Perl returns empty list. We can do the same.
	case "active.times", "distributions", "distrib.pats":
		s.respond(215, fmt.Sprintf("List of %s follows", listType))
		s.sendTextLines([]string{}) // Empty list for now

	default:
		s.respond(501, fmt.Sprintf("LIST type '%s' not understood or not implemented", listType))
	}
}

func (s *Session) handleListGroup(args []string) {
	var groupToUse *config.GroupConfig
	if len(args) > 0 {
		groupName := strings.ToLower(args[0])
		var ok bool
		groupToUse, ok = s.config.Groups[groupName]
		if !ok || groupToUse.Num == 0 {
			s.respond(411, "No such newsgroup")
			return
		}
		// Switch current group context if a group is specified
		s.selectedGroup = groupToUse
	} else {
		if s.selectedGroup == nil {
			s.respond(412, "No newsgroup selected")
			return
		}
		groupToUse = s.selectedGroup
	}

	minArticle, maxArticle, count, err := s.db.GetGroupMinMaxArticleIDs(groupToUse.Num)
	if err != nil {
		log.Printf("[%s] LISTGROUP: DB error for group %s: %v", s.id, groupToUse.Name, err)
		s.respond(500, "Server error retrieving article list")
		return
	}

	// Adjust for NNTP response conventions for empty groups
	firstToList := minArticle
	lastToList := maxArticle
	if count == 0 {
		firstToList = 1
		lastToList = 0
	}


	// 211 count first last name - article list follows
	s.respond(211, fmt.Sprintf("%d %d %d %s list of article numbers follows", count, firstToList, lastToList, groupToUse.Name))

	var lines []string
	if count > 0 {
		for i := minArticle; i <= maxArticle; i++ {
			// TODO: This could be a very long list. Check if article 'i' actually exists if storage is sparse.
			// For now, assuming contiguous article numbers if min/max are valid.
			lines = append(lines, strconv.FormatInt(i, 10))
		}
	}
	s.sendTextLines(lines) // sendTextLines handles the final "."

	// Set current article to the first in the group
	if count > 0 {
	    s.currentArticleNum = minArticle
	} else {
	    s.currentArticleNum = 0 // Or firstToList (1) if that's the convention
	}
}


func (s *Session) handleMode(args []string) {
	if len(args) < 1 {
		s.respond(501, "MODE command requires an argument (e.g., READER)")
		return
	}
	mode := strings.ToLower(args[0])
	if mode == "reader" {
		s.modeReader = true
		// The Perl script's initial banner implies posting is always OK.
		// 200 Posting allowed
		// 201 Posting prohibited
		s.respond(200, "Posting allowed.")
	} else if mode == "stream" {
		// Perl script: respond(200,"sure, why not?"); but comments it's not really supported.
		// We'll formally say it's not supported.
		s.respond(501, "MODE STREAM not supported.") // Or 203 for "program fault - command not performed"
	} else {
		s.respond(501, fmt.Sprintf("Unrecognised mode '%s'", args[0]))
	}
}

func (s *Session) handleNext(args []string) {
	if s.selectedGroup == nil {
		s.respond(412, "No newsgroup selected")
		return
	}
	if s.currentArticleNum == 0 && s.selectedGroup != nil { // If no current article, but group selected, try starting from first
		minArticle, _, count, err := s.db.GetGroupMinMaxArticleIDs(s.selectedGroup.Num)
		if err != nil || count == 0 {
			s.respond(421, "No next article in this group (group empty or error)")
			return
		}
		s.currentArticleNum = minArticle
		// Now fall through to fetch this article's header for the response
	} else if s.currentArticleNum == 0 { // No group, no current article
		s.respond(420, "No current article selected")
		return
	}


	_, maxArticle, _, err := s.db.GetGroupMinMaxArticleIDs(s.selectedGroup.Num)
	if err != nil {
		log.Printf("[%s] DB error in NEXT for group %s: %v", s.id, s.selectedGroup.Name, err)
		s.respond(500, "Server error processing NEXT")
		return
	}

	if s.currentArticleNum >= maxArticle {
		s.respond(421, "No next article in this group")
		return
	}

	s.currentArticleNum++
	// Fetch actual message-id for the response
	header, err := s.db.GetArticleHeaderByNum(s.selectedGroup.Num, s.currentArticleNum)
	if err != nil || header == nil {
		log.Printf("[%s] DB error fetching header for NEXT article %d in %s: %v", s.id, s.currentArticleNum, s.selectedGroup.Name, err)
		s.respond(421, "No next article in this group (or error fetching it)")
		return
	}

	msgIDStr := ""
	if header.MessageID.Valid && header.MessageID.String != "" {
		msgIDStr = header.MessageID.String
	} else {
		msgIDStr = fmt.Sprintf("<%s-%d@%s>", s.selectedGroup.Name, s.currentArticleNum, s.config.ServerName)
	}
	s.respond(223, fmt.Sprintf("%d %s article retrieved - request text separately", s.currentArticleNum, msgIDStr))
}


func (s *Session) handlePost(args []string) {
	// Check if posting is allowed in general (e.g. server-wide config, or modeReader status)
	// The initial banner says "posting ok", so we assume it is.
	s.respond(340, "Send article to be posted. End with <CR-LF>.<CR-LF>")
	s.receiveArticle(false, "", "") // isIhave=false, no pre-known messageID or hash
}

func (s *Session) receiveArticle(isIhave bool, clientProvidedMsgID, clientProvidedHashedMsgID string) {
    // Read headers
    var headersText strings.Builder
    var newsgroupsHeader, fromHeader, messageIDHeader string
    headerBytes := 0

    for {
        s.conn.SetReadDeadline(time.Now().Add(time.Duration(s.config.Timeout) * time.Second)) // Reset timeout for each line
        line, err := s.reader.ReadString('\n')
        if err != nil {
            log.Printf("[%s] Error reading article headers: %v", s.id, err)
            s.respond(441, "Posting failed (read error)") // Or 436 for IHAVE
            return
        }
        headerBytes += len(line)

        // Perl: last if /^(\.)?\r?\n$/s; (ends header reading on empty or dot-only line)
        // Here, we just check for empty line after CRLF normalization.
        normalizedLine := strings.TrimRight(line, "\r\n")
        if normalizedLine == "" { // End of headers
            headersText.WriteString("\r\n") // Add the final CRLF for the header section
            break
        }
        // Perl: s/\r\n$/\n/s; (normalizes to LF, we'll keep CRLF for writing to file/sending)
        // We'll store with CRLF.
        headersText.WriteString(normalizedLine + "\r\n")


        // Extract key headers
        // Perl: ($newsgroups) = /^Newsgroups: (.+)/ unless $newsgroups;
        //       !$from and /^From: (.*)/ and ($from) = (Mail::Address->parse($1))[0]->address;
        //       (similar for Message-ID)
        lowerLine := strings.ToLower(normalizedLine)
        if strings.HasPrefix(lowerLine, "newsgroups:") && newsgroupsHeader == "" {
            newsgroupsHeader = strings.TrimSpace(normalizedLine[len("newsgroups:"):])
        } else if strings.HasPrefix(lowerLine, "from:") && fromHeader == "" {
            fromHeader = strings.TrimSpace(normalizedLine[len("from:"):])
        } else if strings.HasPrefix(lowerLine, "message-id:") && messageIDHeader == "" {
            messageIDHeader = strings.TrimSpace(normalizedLine[len("message-id:"):])
        }
        // TODO: The Perl script also checks for "Cancel:" and a specific spam From address.
    }

    // Validate required headers
    if newsgroupsHeader == "" {
        s.respond(isIhaveResponse(isIhave, 437, 441), "Posting failed - Newsgroups header missing or empty")
        return
    }
    if fromHeader == "" {
        s.respond(isIhaveResponse(isIhave, 437, 441), "Posting failed - From header missing or empty")
        return
    }
    // Basic From validation (Perl: $from =~ m/^.+?@[-.\w]+$/;)
    parsedFrom, err := mail.ParseAddress(fromHeader)
    if err != nil {
        s.respond(isIhaveResponse(isIhave, 437, 441), fmt.Sprintf("Posting failed - Invalid From header format: %v", err))
        return
    }
    _ = parsedFrom // Use if needed, e.g. for mail injection -f flag

    // Determine actual Message-ID and its hash
    actualMsgID := ""      // e.g. foo@bar.com
    actualMsgIDFull := ""  // e.g. <foo@bar.com>

    if messageIDHeader != "" {
        actualMsgIDFull = messageIDHeader
        parsed, ok := parseMessageID(messageIDHeader)
        if ok {
            actualMsgID = parsed
        } else {
            // Invalid Message-ID format from client
            s.respond(isIhaveResponse(isIhave, 437, 441), "Posting failed - Invalid Message-ID header format")
            return
        }
    } else {
        // Generate a message-ID if not provided
        // For POST, server should generate. For IHAVE, it MUST be present.
        if isIhave {
            s.respond(437, "Article rejected - Message-ID header missing for IHAVE")
            return
        }
        // TODO: Generate a unique message ID, e.g., <timestamp.process@servername>
        actualMsgID = fmt.Sprintf("%d.%d@%s", time.Now().UnixNano(), os.Getpid(), s.config.ServerName)
        actualMsgIDFull = "<" + actualMsgID + ">"
        headersText.WriteString("Message-ID: " + actualMsgIDFull + "\r\n") // Add to headers
    }

    // If IHAVE, the clientProvidedMsgID should match actualMsgID derived from headers
    if isIhave && clientProvidedMsgID != actualMsgID {
        log.Printf("[%s] IHAVE msgID mismatch: claimed '%s', header was '%s'", s.id, clientProvidedMsgID, actualMsgID)
        s.respond(437, "Article rejected - Message-ID in IHAVE command does not match Message-ID header")
        // Drain the rest of the article data from client to prevent pipe issues
        s.drainArticleBody()
        return
    }

    hashedActualMsgID := md5Hex(actualMsgID)

    // Re-check if we have this (Race condition if checked before 335 for IHAVE)
    // The Perl script does check before 335 for IHAVE.
    // If this is a POST, or if it's IHAVE and this is the first *real* check after headers.
    if !isIhave { // For POST, check now. For IHAVE, it was checked before 335 based on clientProvidedHashedMsgID.
        exists, dbErr := s.db.CheckHashedMessageIDExists(hashedActualMsgID)
        if dbErr != nil {
            log.Printf("[%s] DB error checking msgid %s for POST: %v", s.id, actualMsgID, dbErr)
            s.respond(441, "Posting failed (server DB error)")
            s.drainArticleBody()
            return
        }
        if exists {
            s.respond(441, "Posting failed (article with this Message-ID already exists)")
            s.drainArticleBody()
            return
        }
    }


    // Read body
    var bodyText strings.Builder
    bodyBytes := 0
    for {
        s.conn.SetReadDeadline(time.Now().Add(time.Duration(s.config.Timeout) * time.Second))
        line, err := s.reader.ReadString('\n')
        if err != nil {
            // EOF might be acceptable if it's after the final ".\r\n"
            // but the loop structure expects to break on ".\r\n"
            log.Printf("[%s] Error reading article body: %v", s.id, err)
            s.respond(isIhaveResponse(isIhave, 436, 441), "Posting failed (body read error)")
            return
        }

        // Check for end-of-message marker ".\r\n"
        // Perl: last if /^\.\n$/s; (after s/\r\n$/\n/s;)
        // So, it expects ".\n" after normalization.
        if line == ".\r\n" || line == ".\n" {
            break
        }

        // Perl: s/^\.//g; (unescape leading dots) - This is for storage/processing, not for what's received.
        // The line received from client is taken as is.
        bodyText.WriteString(line) // Keep original line endings from client
        bodyBytes += len(line)
    }

    fullArticleContent := headersText.String() + bodyText.String()
    totalBytes := headerBytes + bodyBytes // Approximate, depends on exact processing

    // TODO: Process the article:
    // 1. Store it (filesystem and/or database). This needs a new article ID within each target group.
    //    - The Perl script uses a mail injection command which implies it might also be sent to a mailing list.
    //    - `open(FILE, "|-") || exec @mailinject, "-f$from", @mailto;`
    //    - Then prints headers and body to this pipe.
    //    - For DB storage, it would parse headers again from the raw article to populate h_* fields.
    // 2. Update database records (articles table, overview).

    log.Printf("[%s] Received article: %s, Newsgroups: %s, From: %s, Bytes: %d", s.id, actualMsgIDFull, newsgroupsHeader, fromHeader, totalBytes)

    // Placeholder for actual storage and DB update logic
    // For now, just acknowledge.
    targetNewsgroups := strings.Split(strings.ToLower(strings.ReplaceAll(newsgroupsHeader, " ", "")), ",")
    if len(targetNewsgroups) == 0 {
         s.respond(isIhaveResponse(isIhave, 437, 441), "Posting failed - No valid newsgroups found in Newsgroups header")
         return
    }
    log.Printf("[%s] TODO: Store article for groups: %v", s.id, targetNewsgroups)


    // Final response
    s.respond(isIhaveResponse(isIhave, 235, 240), "Article posted ok")
}

// drainArticleBody reads and discards the rest of an article body
// typically after an error has occurred post-335/340 response.
func (s *Session) drainArticleBody() {
    log.Printf("[%s] Draining article body from client.", s.id)
    for {
        s.conn.SetReadDeadline(time.Now().Add(time.Duration(s.config.Timeout) * time.Second)) // Short timeout for draining
        line, err := s.reader.ReadString('\n')
        if err != nil {
            log.Printf("[%s] Error/EOF while draining article body: %v", s.id, err)
            return // EOF or other error, stop draining
        }
        if line == ".\r\n" || line == ".\n" {
            log.Printf("[%s] Finished draining article body.", s.id)
            return // End of article marker found
        }
    }
}


// isIhaveResponse returns the appropriate response code based on whether the command was IHAVE or POST.
func isIhaveResponse(isIhave bool, ihaveCode, postCode int) int {
    if isIhave {
        return ihaveCode
    }
    return postCode
}


func (s *Session) handleQuit(args []string) {
	s.respond(205, "Closing connection - goodbye!")
	// The Serve() loop will terminate after this.
}

func (s *Session) handleSlave(args []string) {
	// Perl: respond(202, "slave status noted"); # yeah, not really. nobody cares.
	s.respond(202, "Slave status noted (but not acted upon)")
}

func (s *Session) handleXVersion(args []string) {
	// Perl: respond(200, "Colobus $Colobus::VERSION");
	s.respond(200, fmt.Sprintf("Colobus-Go %s", s.serverVersion)) // Use the defined server version
}


func (s *Session) handleXPath(args []string) {
	if len(args) < 1 {
		s.respond(501, "XPATH requires a message-ID argument")
		return
	}
	msgIDArg := args[0]
	parsedMsgID, ok := parseMessageID(msgIDArg)
	if !ok {
		s.respond(501, "Invalid message-ID format for XPATH")
		return
	}

	hashedMsgID := md5Hex(parsedMsgID)
	// Perl: my ($ggg,$begin) = get_group_and_article($1); respond(223, "$ggg/$begin");
	// get_group_and_article first tries DB, then parses group-num@server

	dbGroupID, dbArticleNum, found, err := s.db.GetArticleGroupAndNumByHashedMsgID(hashedMsgID)
	if err != nil {
		log.Printf("[%s] XPATH DB error for msgid %s: %v", s.id, parsedMsgID, err)
		s.respond(503, "Server error processing XPATH") // Or 430 no such article
		return
	}
	if !found {
		// TODO: Implement fallback parsing of "group-articlenumber@servername" from parsedMsgID
		// For now, if not in DB, it's not found.
		s.respond(430, "No such article found by XPATH")
		return
	}

	groupName, ok := s.groupNumToName[dbGroupID]
	if !ok {
		log.Printf("[%s] XPATH found article %d in unknown group ID %d", s.id, dbArticleNum, dbGroupID)
		s.respond(430, "No such article found (group config error)")
		return
	}

	// 223 article-number path (for now, path is just groupname/articlenum)
	s.respond(223, fmt.Sprintf("%d %s/%d", dbArticleNum, groupName, dbArticleNum))
}

func (s *Session) handleXover(args []string) {
	if s.selectedGroup == nil {
		s.respond(412, "No newsgroup selected for XOVER")
		return
	}

	var startNum, endNum int64
	minArticle, maxArticle, count, err := s.db.GetGroupMinMaxArticleIDs(s.selectedGroup.Num)
	if err != nil {
		log.Printf("[%s] XOVER: DB error getting group info for %s: %v", s.id, s.selectedGroup.Name, err)
		s.respond(500, "Server error processing XOVER")
		return
	}

	if count == 0 {
		s.respond(224, "Overview information follows") // Standard response for empty range
		s.sendTextLines([]string{}) // Empty list + "."
		return
	}

	if len(args) == 0 { // No range, use current article
		if s.currentArticleNum == 0 || s.currentArticleNum < minArticle || s.currentArticleNum > maxArticle {
			// If current is not set or invalid, XOVER on current is not well-defined.
			// Some servers might default to last article, some error.
			// Let's default to the current article if valid, or first if current is not set.
			if s.currentArticleNum >= minArticle && s.currentArticleNum <=maxArticle {
				startNum = s.currentArticleNum
				endNum = s.currentArticleNum
			} else {
				startNum = minArticle
				endNum = minArticle
			}

		} else {
			startNum = s.currentArticleNum
			endNum = s.currentArticleNum
		}
	} else { // Range specified: "start-[end]"
		rangeParts := strings.Split(args[0], "-")
		if len(rangeParts[0]) > 0 {
			startNum, err = strconv.ParseInt(rangeParts[0], 10, 64)
			if err != nil {
				s.respond(501, "Invalid start of range for XOVER")
				return
			}
		} else { // e.g. "-end" means min to end
			startNum = minArticle
		}

		if len(rangeParts) > 1 && len(rangeParts[1]) > 0 {
			endNum, err = strconv.ParseInt(rangeParts[1], 10, 64)
			if err != nil {
				s.respond(501, "Invalid end of range for XOVER")
				return
			}
		} else { // "start-" or just "start"
			endNum = maxArticle
			if len(rangeParts[0]) > 0 && len(rangeParts) == 1 { // just "start"
			    endNum = startNum
			}
		}
	}

	// Validate range against group's actual min/max
	if startNum < minArticle { startNum = minArticle }
	if endNum > maxArticle { endNum = maxArticle }
	if startNum > endNum { // Invalid or empty range resulting from validation
		s.respond(224, "Overview information follows")
		s.sendTextLines([]string{})
		return
	}

	s.respond(224, fmt.Sprintf("Overview information for %d-%d follows", startNum, endNum))

	// Fetch headers from DB
	// Perl: my (@xover) = get_article_xover($group, $begin, $last);
	// Loop in chunks if necessary (Perl does this with $last = $last + 5000)
	// For now, direct fetch.
	dbHeaders, err := s.db.GetArticleHeaders(s.selectedGroup.Num, startNum, endNum)
	if err != nil {
		log.Printf("[%s] XOVER: DB error fetching headers for %s (%d-%d): %v", s.id, s.selectedGroup.Name, startNum, endNum, err)
		s.sendTextLines([]string{}) // Send empty list on error after 224
		return
	}

	var lines []string
	for _, h := range dbHeaders {
		// Format: articleNumber<TAB>Subject<TAB>From<TAB>Date<TAB>Message-ID<TAB>References<TAB>ByteCount<TAB>LineCount<TAB>Xref:full
		// Xref needs to be constructed.
		var xrefParts []string
		var newsgroupsParts []string // Not directly in XOVER line, but good to have for Xref construction

		if h.HashedMsgID != "" {
			xrefs, dbErr := s.db.GetArticleXrefsByHashedMsgID(h.HashedMsgID)
			if dbErr != nil {
				log.Printf("[%s] XOVER: DB error getting xrefs for article %d (msgid %s): %v", s.id, h.ID, h.HashedMsgID, dbErr)
			} else if len(xrefs) > 0 {
				for gID, aID := range xrefs {
					gName, ok := s.groupNumToName[gID]
					if ok {
						xrefParts = append(xrefParts, fmt.Sprintf("%s:%d", gName, aID))
						newsgroupsParts = append(newsgroupsParts, gName) // Keep for consistency
					}
				}
			}
		}
		// Fallback Xref if primary lookup failed or no other groups
		if len(xrefParts) == 0 {
			xrefParts = append(xrefParts, fmt.Sprintf("%s:%d", s.selectedGroup.Name, h.ID))
		}
		fullXref := s.config.ServerName + " " + strings.Join(xrefParts, " ")

		// Order from s.config.OverviewFields should be respected here.
		// Subject From Date Message-ID References Bytes Lines
		// Example line: 1\tSubject\tFrom\tDate\t<msgid>\t<refs>\t123\t10\tXref: server group:1
		// Note: Perl script joins with \t. Empty fields are empty strings.
		line := fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s",
			h.ID,
			h.Subject.String, // NullString.String is empty if null
			h.From.String,
			h.Date.String,
			h.MessageID.String, // This is the <actual-id>
			h.References.String,
			h.Bytes.Int64,    // NullInt64.Int64 is 0 if null
			h.Lines.Int64,
			fullXref, // Xref is not one of the @overview fields in Perl, it's added at the end
		)
		lines = append(lines, line)
	}
	s.sendTextLines(lines)
}


func (s *Session) handleXhdr(command string, args []string) {
    // XHDR header [range|<messageid>]
    // XPAT header pattern [range|<messageid>] (Perl sends to xhdr with code 221)
    // XROVER [range|<messageid>] (Perl sends to xhdr for "References" header with code 224)

    if len(args) < 1 {
        s.respond(501, fmt.Sprintf("%s requires a header name", strings.ToUpper(command)))
        return
    }

    headerNameArg := args[0]
    var targetRangeOrMsgID string
    if len(args) > 1 {
        targetRangeOrMsgID = args[1]
    }
    // For XPAT, patterns are args[2:]

    // Normalize command-specifics
    targetHeaderName := ""
    responseCode := 0
    // var patterns []string // For XPAT

    switch command {
    case "xhdr":
        targetHeaderName = headerNameArg
        responseCode = 221 // "Headers follow"
    case "xpat":
        targetHeaderName = headerNameArg
        responseCode = 221 // Perl uses 221 for XPAT
        // patterns = args[2:]
		s.respond(501, "XPAT not fully implemented yet (pattern matching)")
		return // TODO: Implement XPAT pattern matching
    case "xrover":
        targetHeaderName = "References" // XROVER is XHDR References
        responseCode = 224      // "Overview information follows" (like XOVER)
        if len(args) > 0 { // XROVER's first arg is range/msgid
            targetRangeOrMsgID = args[0]
        }
    default:
        s.respond(500, "Internal error: Unhandled command in xhdr_family")
        return
    }

    // Check if header is in overview.fmt (Perl: exists $overview{$header})
    // For simplicity, we'll allow any header but it might be slow if not indexed.
    // The Perl script short-circuits if header not in @overview.
    isOverviewHeader := false
    for _, ovh := range s.config.OverviewFields {
        if strings.EqualFold(ovh, targetHeaderName) {
            // Canonicalize header name like Perl: $header =~ s/^$_$/$_/i;
            targetHeaderName = ovh
            isOverviewHeader = true
            break
        }
    }
    if !isOverviewHeader && !strings.EqualFold(targetHeaderName, "Xref") { // Xref is special
         s.respond(responseCode, fmt.Sprintf("%s not an overview header, returning empty list", targetHeaderName))
         s.sendTextLines([]string{})
         return
    }


    var articlesToQuery []database.ArticleHeader
    var queryErr error

    if strings.HasPrefix(targetRangeOrMsgID, "<") && strings.HasSuffix(targetRangeOrMsgID, ">") { // Message-ID
        parsedMsgID, ok := parseMessageID(targetRangeOrMsgID)
        if !ok {
            s.respond(501, "Invalid message-ID format for "+strings.ToUpper(command))
            return
        }
        hashedMsgID := md5Hex(parsedMsgID)
        dbGroupID, dbArticleNum, found, err := s.db.GetArticleGroupAndNumByHashedMsgID(hashedMsgID)
        if err != nil || !found {
            s.respond(430, "No such article found by message-ID")
            return
        }
        // Now get the header for this specific article
        artHeader, err := s.db.GetArticleHeaderByNum(dbGroupID, dbArticleNum)
        if err != nil || artHeader == nil {
            s.respond(430, "No such article found (header fetch failed)")
            return
        }
        articlesToQuery = append(articlesToQuery, *artHeader)
    } else { // Range in current group
        if s.selectedGroup == nil {
            s.respond(412, "No newsgroup selected for "+strings.ToUpper(command)+" range")
            return
        }
        minDb, maxDb, countDb, errDb := s.db.GetGroupMinMaxArticleIDs(s.selectedGroup.Num)
        if errDb != nil || countDb == 0 {
            s.respond(responseCode, fmt.Sprintf("%s follows", targetHeaderName))
            s.sendTextLines([]string{}) // Empty list
            return
        }

        startNum, endNum := minDb, maxDb // Default to full range
        if targetRangeOrMsgID != "" { // A range is specified
            rangeParts := strings.Split(targetRangeOrMsgID, "-")
            parsedStart, errStart := strconv.ParseInt(rangeParts[0], 10, 64)
            if errStart == nil {
                startNum = parsedStart
            }
            if len(rangeParts) > 1 && rangeParts[1] != "" {
                parsedEnd, errEnd := strconv.ParseInt(rangeParts[1], 10, 64)
                if errEnd == nil {
                    endNum = parsedEnd
                }
            } else if len(rangeParts) == 1 { // Only start specified, so range is just that one article
                endNum = startNum
            }
        } else { // No range specified, use current article
             if s.currentArticleNum >= minDb && s.currentArticleNum <= maxDb {
                startNum = s.currentArticleNum
                endNum = s.currentArticleNum
            } else { // Current article not valid or not set, maybe default to first or error
                s.respond(420, "No current article, and no range/messageID specified")
                return
            }
        }

        // Clamp range
        if startNum < minDb { startNum = minDb }
        if endNum > maxDb { endNum = maxDb }
        if startNum > endNum {
             s.respond(responseCode, fmt.Sprintf("%s follows", targetHeaderName))
             s.sendTextLines([]string{}) // Empty list
             return
        }
        articlesToQuery, queryErr = s.db.GetArticleHeaders(s.selectedGroup.Num, startNum, endNum)
        if queryErr != nil {
            log.Printf("[%s] %s: DB error fetching headers: %v", s.id, strings.ToUpper(command), queryErr)
            s.respond(responseCode, fmt.Sprintf("%s follows", targetHeaderName))
            s.sendTextLines([]string{})
            return
        }
    }

    s.respond(responseCode, fmt.Sprintf("%s follows", targetHeaderName))
    var lines []string
    for _, h := range articlesToQuery {
        var headerValue string
        // Extract the requested header value
        // This is case-sensitive for struct fields, but we canonicalized targetHeaderName
        // to match one of s.config.OverviewFields or "Xref"
        switch targetHeaderName {
        case "Subject":    headerValue = h.Subject.String
        case "From":       headerValue = h.From.String
        case "Date":       headerValue = h.Date.String
        case "Message-ID": headerValue = h.MessageID.String // This is the <actual-id>
        case "References": headerValue = h.References.String
        case "Bytes":      if h.Bytes.Valid { headerValue = strconv.FormatInt(h.Bytes.Int64, 10) }
        case "Lines":      if h.Lines.Valid { headerValue = strconv.FormatInt(h.Lines.Int64, 10) }
        case "Xref":
            var xrefParts []string
            if h.HashedMsgID != "" {
                xrefs, _ := s.db.GetArticleXrefsByHashedMsgID(h.HashedMsgID)
                if len(xrefs) > 0 {
                    for gID, aID := range xrefs {
                        if gName, ok := s.groupNumToName[gID]; ok {
                            xrefParts = append(xrefParts, fmt.Sprintf("%s:%d", gName, aID))
                        }
                    }
                }
            }
             if len(xrefParts) == 0 { // Fallback
                groupNameForH, _ := s.groupNumToName[h.GroupID] // h.GroupID should be set
                xrefParts = append(xrefParts, fmt.Sprintf("%s:%d", groupNameForH, h.ID))
            }
            headerValue = s.config.ServerName + " " + strings.Join(xrefParts, " ")
        default:
            // This case should have been caught by the overview check, but as a fallback:
            // Potentially fetch raw article and parse header if not overview. (Very slow)
            // For now, if it's not an overview field, it's empty.
            headerValue = ""
        }

        // TODO: Implement XPAT pattern matching against headerValue if command is "xpat"
        // For now, XPAT without patterns acts like XHDR.

        if headerValue != "" { // Only include if header exists and has value
            lines = append(lines, fmt.Sprintf("%d %s", h.ID, headerValue))
        } else {
            lines = append(lines, fmt.Sprintf("%d ", h.ID)) // Article num and a space if header is empty/missing
        }
    }
    s.sendTextLines(lines)
}
