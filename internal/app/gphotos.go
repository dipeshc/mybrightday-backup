package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const (
	photosUploadURL     = "https://photoslibrary.googleapis.com/v1/uploads"
	photosMediaItemsURL = "https://photoslibrary.googleapis.com/v1/mediaItems:batchCreate"
	photosAlbumsURL     = "https://photoslibrary.googleapis.com/v1/albums"
	photosSearchURL     = "https://photoslibrary.googleapis.com/v1/mediaItems:search"
)

// attachmentIDFromFilenameRe extracts a MongoDB ObjectId from filenames like daycare_2024-01-15_69f9390f8d9c1412adacc127.jpg.
var attachmentIDFromFilenameRe = regexp.MustCompile(
	`^daycare_\d{4}-\d{2}-\d{2}_([0-9a-fA-F]{24})\.jpg$`,
)

// batchCreateRequest is the request body for creating media items.
type batchCreateRequest struct {
	AlbumID       string         `json:"albumId,omitempty"`
	NewMediaItems []newMediaItem `json:"newMediaItems"`
}

// newMediaItem represents a single media item to create.
type newMediaItem struct {
	SimpleMediaItem simpleMediaItem `json:"simpleMediaItem"`
}

// simpleMediaItem holds the upload token and filename.
type simpleMediaItem struct {
	UploadToken string `json:"uploadToken"`
	FileName    string `json:"fileName"`
}

// batchCreateResponse is the response from the media items batch create endpoint.
type batchCreateResponse struct {
	NewMediaItemResults []mediaItemResult `json:"newMediaItemResults"`
}

// mediaItemResult holds the result of creating a single media item.
type mediaItemResult struct {
	UploadToken string    `json:"uploadToken"`
	Status      apiStatus `json:"status"`
	MediaItem   mediaItem `json:"mediaItem"`
}

// apiStatus represents the status of an API operation.
type apiStatus struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// mediaItem represents a created media item.
type mediaItem struct {
	ID string `json:"id"`
}

// albumEntry represents a Google Photos album from the API.
type albumEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// albumsListResponse is the response from the albums list endpoint.
type albumsListResponse struct {
	Albums        []albumEntry `json:"albums"`
	NextPageToken string       `json:"nextPageToken"`
}

// createAlbumRequest is the request body for creating an album.
type createAlbumRequest struct {
	Album createAlbumBody `json:"album"`
}

// createAlbumBody holds the album title for creation.
type createAlbumBody struct {
	Title string `json:"title"`
}

// searchMediaItemsRequest is the request body for searching media items in an album.
type searchMediaItemsRequest struct {
	AlbumID   string `json:"albumId"`
	PageSize  int    `json:"pageSize"`
	PageToken string `json:"pageToken,omitempty"`
}

// searchMediaItemsResponse is the response from the media items search endpoint.
type searchMediaItemsResponse struct {
	MediaItems    []mediaItemEntry `json:"mediaItems"`
	NextPageToken string           `json:"nextPageToken"`
}

// mediaItemEntry represents a media item returned by the search endpoint.
type mediaItemEntry struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
}

// findOrCreateAlbum finds an existing album by title or creates a new one.
// Returns the album ID.
func findOrCreateAlbum(ctx context.Context, client *http.Client, title string) (string, error) {
	albumID, err := findAlbumByTitle(ctx, client, title)
	if err != nil {
		return "", fmt.Errorf("searching for album: %w", err)
	}
	if albumID != "" {
		return albumID, nil
	}

	albumID, err = createAlbum(ctx, client, title)
	if err != nil {
		return "", fmt.Errorf("creating album: %w", err)
	}

	return albumID, nil
}

// findAlbumByTitle paginates through all albums and returns the ID of the first
// album matching the given title. Returns empty string if not found.
func findAlbumByTitle(ctx context.Context, client *http.Client, title string) (string, error) {
	pageToken := ""

	for {
		reqURL := photosAlbumsURL + "?pageSize=50"
		if pageToken != "" {
			reqURL += "&pageToken=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return "", fmt.Errorf("creating albums list request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("executing albums list request: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("reading albums list response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("albums list failed with status %d: %s", resp.StatusCode, string(body))
		}

		var listResp albumsListResponse
		if err := json.Unmarshal(body, &listResp); err != nil {
			return "", fmt.Errorf("parsing albums list response: %w", err)
		}

		for _, album := range listResp.Albums {
			if strings.EqualFold(album.Title, title) {
				return album.ID, nil
			}
		}

		if listResp.NextPageToken == "" {
			break
		}
		pageToken = listResp.NextPageToken
	}

	return "", nil
}

// createAlbum creates a new Google Photos album with the given title.
// Returns the album ID.
func createAlbum(ctx context.Context, client *http.Client, title string) (string, error) {
	reqBody := createAlbumRequest{
		Album: createAlbumBody{Title: title},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling create album request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", photosAlbumsURL, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("creating album create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing album create request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading album create response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("album creation failed with status %d: %s", resp.StatusCode, string(body))
	}

	var album albumEntry
	if err := json.Unmarshal(body, &album); err != nil {
		return "", fmt.Errorf("parsing album create response: %w", err)
	}

	return album.ID, nil
}

// listAlbumAttachmentIDs paginates through all media items in an album and extracts
// attachment IDs from filenames matching the daycare_*_<24hexID>.jpg pattern.
// Returns a set of attachment IDs already present in the album.
func listAlbumAttachmentIDs(ctx context.Context, client *http.Client, albumID string) (map[string]bool, error) {
	ids := make(map[string]bool)
	pageToken := ""

	for {
		searchReq := searchMediaItemsRequest{
			AlbumID:   albumID,
			PageSize:  100,
			PageToken: pageToken,
		}

		jsonData, err := json.Marshal(searchReq)
		if err != nil {
			return nil, fmt.Errorf("marshaling search request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", photosSearchURL, bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("creating search request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("executing search request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading search response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("search failed with status %d: %s", resp.StatusCode, string(body))
		}

		var searchResp searchMediaItemsResponse
		if err := json.Unmarshal(body, &searchResp); err != nil {
			return nil, fmt.Errorf("parsing search response: %w", err)
		}

		for _, item := range searchResp.MediaItems {
			if matches := attachmentIDFromFilenameRe.FindStringSubmatch(item.Filename); matches != nil {
				ids[matches[1]] = true
			}
		}

		if searchResp.NextPageToken == "" {
			break
		}
		pageToken = searchResp.NextPageToken
	}

	return ids, nil
}

// uploadToPhotos uploads JPEG data to Google Photos and creates a media item
// in the specified album. Returns the media item ID on success.
func uploadToPhotos(ctx context.Context, client *http.Client, jpegData []byte, filename, albumID string) (string, error) {
	// Step 1: Upload the raw bytes.
	uploadToken, err := uploadBytes(ctx, client, jpegData, filename)
	if err != nil {
		return "", fmt.Errorf("uploading bytes: %w", err)
	}

	// Step 2: Create the media item in the album.
	mediaItemID, err := createMediaItem(ctx, client, uploadToken, filename, albumID)
	if err != nil {
		return "", fmt.Errorf("creating media item: %w", err)
	}

	return mediaItemID, nil
}

// uploadBytes uploads raw image bytes to Google Photos and returns an upload token.
func uploadBytes(ctx context.Context, client *http.Client, data []byte, filename string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", photosUploadURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating upload request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Goog-Upload-File-Name", filename)
	req.Header.Set("X-Goog-Upload-Protocol", "raw")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing upload request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading upload response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	// The response body is the upload token as plain text.
	return string(body), nil
}

// createMediaItem creates a media item in Google Photos from an upload token,
// optionally adding it to an album.
func createMediaItem(ctx context.Context, client *http.Client, uploadToken, filename, albumID string) (string, error) {
	reqBody := batchCreateRequest{
		AlbumID: albumID,
		NewMediaItems: []newMediaItem{
			{
				SimpleMediaItem: simpleMediaItem{
					UploadToken: uploadToken,
					FileName:    filename,
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", photosMediaItemsURL, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("creating media item request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing media item request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading media item response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("media item creation failed with status %d: %s", resp.StatusCode, string(body))
	}

	var batchResp batchCreateResponse
	if err := json.Unmarshal(body, &batchResp); err != nil {
		return "", fmt.Errorf("parsing media item response: %w", err)
	}

	if len(batchResp.NewMediaItemResults) == 0 {
		return "", fmt.Errorf("no media item results in response")
	}

	result := batchResp.NewMediaItemResults[0]
	if result.Status.Code != 0 {
		return "", fmt.Errorf("media item creation error: %s (code %d)", result.Status.Message, result.Status.Code)
	}

	return result.MediaItem.ID, nil
}
