package mimeparser

import (
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto" // For textproto.MIMEHeader
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

var ugcPolicy *bluemonday.Policy

func init() {
	ugcPolicy = bluemonday.UGCPolicy()
	ugcPolicy.AllowElements("pre")
}

// SanitizeHTML cleans HTML content using a UGC policy.
func SanitizeHTML(htmlContent string) string {
	return ugcPolicy.Sanitize(htmlContent)
}

// ParsedArticleContent holds the extracted and processed content from an article.
type ParsedArticleContent struct {
	TextBody        string
	HTMLBody        string
	SanitizedHTML   string
	PreferredBody   string
	IsHTML          bool
	OtherPartsExist bool
}

// ParseArticle parses a raw email message and extracts text/plain and text/html parts.
func ParseArticle(rawArticle io.Reader) (*ParsedArticleContent, error) {
	msg, err := mail.ReadMessage(rawArticle)
	if err != nil {
		return nil, fmt.Errorf("failed to parse email message: %w", err)
	}

	parsedContent := &ParsedArticleContent{}
	var foundTextPlain, foundTextHTML bool

	contentTypeHeader := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentTypeHeader)
	if err != nil {
		log.Printf("Warning: could not parse Content-Type header '%s': %v. Assuming text/plain.", contentTypeHeader, err)
		bodyBytes, readErr := io.ReadAll(msg.Body)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read message body with unparseable Content-Type: %w", readErr)
		}
		parsedContent.TextBody = string(bodyBytes)
		parsedContent.PreferredBody = parsedContent.TextBody
		parsedContent.IsHTML = false
		return parsedContent, nil
	}

	if strings.HasPrefix(mediaType, "text/plain") {
		bodyBytes, err := readPartBody(msg.Body, textproto.MIMEHeader(msg.Header)) // Cast mail.Header
		if err != nil {
			return nil, fmt.Errorf("failed to read text/plain body: %w", err)
		}
		parsedContent.TextBody = string(bodyBytes)
		foundTextPlain = true
	} else if strings.HasPrefix(mediaType, "text/html") {
		bodyBytes, err := readPartBody(msg.Body, textproto.MIMEHeader(msg.Header)) // Cast mail.Header
		if err != nil {
			return nil, fmt.Errorf("failed to read text/html body: %w", err)
		}
		parsedContent.HTMLBody = string(bodyBytes)
		foundTextHTML = true
	} else if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(msg.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("failed to read multipart part: %w", err)
			}
			defer part.Close()

			partMediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil {
				log.Printf("Warning: could not parse Content-Type for a part: %v. Skipping part.", err)
				parsedContent.OtherPartsExist = true
				continue
			}

			if strings.HasPrefix(partMediaType, "text/plain") && !foundTextPlain {
				bodyBytes, err := readPartBody(part, part.Header) // part.Header is textproto.MIMEHeader
				if err != nil {
					log.Printf("Warning: failed to read text/plain part body: %v", err)
					continue
				}
				parsedContent.TextBody = string(bodyBytes)
				foundTextPlain = true
			} else if strings.HasPrefix(partMediaType, "text/html") && !foundTextHTML {
				bodyBytes, err := readPartBody(part, part.Header) // part.Header is textproto.MIMEHeader
				if err != nil {
					log.Printf("Warning: failed to read text/html part body: %v", err)
					continue
				}
				parsedContent.HTMLBody = string(bodyBytes)
				foundTextHTML = true
			} else {
				filename := part.FileName()
				if filename != "" {
					log.Printf("Found part with filename (potential attachment): %s, Content-Type: %s", filename, partMediaType)
				} else {
					log.Printf("Found other multipart part, Content-Type: %s", partMediaType)
				}
				parsedContent.OtherPartsExist = true
			}
		}
	} else {
		log.Printf("Message with non-text/non-multipart Content-Type: %s", mediaType)
		parsedContent.OtherPartsExist = true
		bodyBytes, readErr := io.ReadAll(msg.Body)
		if readErr == nil && len(bodyBytes) > 0 {
			parsedContent.TextBody = string(bodyBytes)
			log.Printf("Read body of non-text/non-multipart message as plain text fallback.")
		}
	}

	if foundTextHTML && parsedContent.HTMLBody != "" {
		parsedContent.SanitizedHTML = SanitizeHTML(parsedContent.HTMLBody)
		parsedContent.PreferredBody = parsedContent.SanitizedHTML
		parsedContent.IsHTML = true
	} else if foundTextPlain && parsedContent.TextBody != "" {
		parsedContent.PreferredBody = parsedContent.TextBody
		parsedContent.IsHTML = false
	} else if parsedContent.TextBody != "" {
		parsedContent.PreferredBody = parsedContent.TextBody
		parsedContent.IsHTML = false
	} else {
		log.Println("No suitable text/plain or text/html part found.")
	}

	return parsedContent, nil
}

// readPartBody handles decoding and reading the body of a MIME part.
func readPartBody(part io.Reader, header textproto.MIMEHeader) ([]byte, error) {
	encoding := header.Get("Content-Transfer-Encoding")
	var reader io.Reader = part

	switch strings.ToLower(encoding) {
	case "quoted-printable":
		reader = quotedprintable.NewReader(reader)
	case "base64":
		// Standard library multipart.Reader and mail.ReadMessage typically handle this.
		break
	default:
	}

	charsetHeader := header.Get("Content-Type")
	if charsetHeader != "" {
		_, params, err := mime.ParseMediaType(charsetHeader)
		if err == nil {
			if ch, ok := params["charset"]; ok {
				log.Printf("Part has charset: %s. (Conversion not yet implemented, assuming UTF-8 compatible)", ch)
			}
		}
	}

	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return bodyBytes, nil
}
