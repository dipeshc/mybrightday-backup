package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// newGmailService creates a Gmail API service from an authenticated HTTP client.
func newGmailService(ctx context.Context, client *http.Client) (*gmail.Service, error) {
	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("creating Gmail service: %w", err)
	}
	return srv, nil
}

// searchEmails finds emails in the inbox matching the configured sender and subject.
func searchEmails(ctx context.Context, srv *gmail.Service, cfg *Config) ([]*gmail.Message, error) {
	query := fmt.Sprintf(
		`in:inbox from:%s subject:"%s"`,
		cfg.Email.Sender,
		cfg.Email.SubjectPattern,
	)

	var messages []*gmail.Message
	pageToken := ""

	for {
		call := srv.Users.Messages.List("me").Q(query).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("listing messages: %w", err)
		}

		messages = append(messages, resp.Messages...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return messages, nil
}

// EmailData holds the parsed content of a fetched email.
type EmailData struct {
	ID       string
	Subject  string
	Date     time.Time
	HTMLBody string
}

// fetchEmail retrieves a full email message and extracts the HTML body and date.
func fetchEmail(ctx context.Context, srv *gmail.Service, id string) (*EmailData, error) {
	msg, err := srv.Users.Messages.Get("me", id).Format("full").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("fetching message %s: %w", id, err)
	}

	subject := extractSubject(msg)
	emailDate := extractDate(msg)
	htmlBody := extractHTMLBody(msg.Payload)

	if htmlBody == "" {
		return nil, fmt.Errorf("no HTML body found in message %s", id)
	}

	return &EmailData{
		ID:       id,
		Subject:  subject,
		Date:     emailDate,
		HTMLBody: htmlBody,
	}, nil
}

// extractSubject returns the Subject header value from an email message.
func extractSubject(msg *gmail.Message) string {
	for _, header := range msg.Payload.Headers {
		if strings.EqualFold(header.Name, "Subject") {
			return header.Value
		}
	}
	return ""
}

// extractDate parses the Date header from an email message.
func extractDate(msg *gmail.Message) time.Time {
	for _, header := range msg.Payload.Headers {
		if strings.EqualFold(header.Name, "Date") {
			// Try common email date formats.
			formats := []string{
				time.RFC1123Z,
				time.RFC1123,
				"Mon, 2 Jan 2006 15:04:05 -0700",
				"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
				"2 Jan 2006 15:04:05 -0700",
			}
			for _, f := range formats {
				if t, err := time.Parse(f, header.Value); err == nil {
					return t
				}
			}
		}
	}

	// Fallback: use the internal timestamp (epoch milliseconds).
	return time.UnixMilli(msg.InternalDate)
}

// extractHTMLBody recursively searches the MIME payload for the text/html part.
func extractHTMLBody(part *gmail.MessagePart) string {
	if part.MimeType == "text/html" {
		data, err := base64.URLEncoding.DecodeString(part.Body.Data)
		if err != nil {
			return ""
		}
		return string(data)
	}

	for _, child := range part.Parts {
		if result := extractHTMLBody(child); result != "" {
			return result
		}
	}

	return ""
}
