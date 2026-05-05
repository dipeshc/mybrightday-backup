package app

import (
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// snapshotPathPrefix is the URL path prefix for Bright Horizons snapshot images.
const snapshotPathPrefix = "/api/parent/medias/v1/media/m/snapshot/"

// snapshotHost is the hostname serving Bright Horizons media.
const snapshotHost = "mbdgw.brighthorizons.com"

// SnapshotImage holds a full-size download URL and its source system UUID.
type SnapshotImage struct {
	// URL is the full-size download URL for the image.
	URL string
	// UUID is the snapshot identifier from Bright Horizons.
	UUID string
}

// extractImageURLs parses the HTML email body and returns snapshot images.
// It finds <img> tags whose src matches the Bright Horizons snapshot URL pattern,
// extracts the snapshot UUID, and constructs a full-size download URL.
func extractImageURLs(htmlBody string) ([]SnapshotImage, error) {
	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	seen := make(map[string]bool)
	var images []SnapshotImage

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			src := getAttr(n, "src")
			if src != "" {
				if img, ok := parseSnapshotURL(src); ok {
					if !seen[img.UUID] {
						seen[img.UUID] = true
						images = append(images, img)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return images, nil
}

// parseSnapshotURL checks if a URL matches the Bright Horizons snapshot pattern
// and returns a SnapshotImage with the full-size URL and UUID.
// Returns ok=false if the URL does not match.
//
// Input example:
//
//	https://mbdgw.brighthorizons.com/api/parent/medias/v1/media/m/snapshot/87589cc7-2187-483e-b26c-397968daeeda?d=t&enail=true
//
// Output:
//
//	SnapshotImage{
//	    URL:  "https://mbdgw.brighthorizons.com/api/parent/medias/v1/media/m/snapshot/87589cc7-2187-483e-b26c-397968daeeda?d=t",
//	    UUID: "87589cc7-2187-483e-b26c-397968daeeda",
//	}
func parseSnapshotURL(rawURL string) (SnapshotImage, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return SnapshotImage{}, false
	}

	if !strings.EqualFold(parsed.Host, snapshotHost) {
		return SnapshotImage{}, false
	}

	if !strings.HasPrefix(parsed.Path, snapshotPathPrefix) {
		return SnapshotImage{}, false
	}

	// Extract the UUID portion after the prefix.
	uuid := strings.TrimPrefix(parsed.Path, snapshotPathPrefix)
	uuid = strings.TrimRight(uuid, "/")
	if uuid == "" {
		return SnapshotImage{}, false
	}

	// Build the full-size image URL with only the ?d=t parameter.
	fullURL := fmt.Sprintf("https://%s%s%s?d=t", snapshotHost, snapshotPathPrefix, uuid)
	return SnapshotImage{URL: fullURL, UUID: uuid}, true
}

// getAttr returns the value of the named attribute on an HTML node.
func getAttr(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}
