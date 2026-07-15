package googlephotos

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rtFunc adapts a function to http.RoundTripper so tests can serve canned
// responses for the hardcoded googleapis URLs.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubClient(handler func(req *http.Request) (status int, body any)) *http.Client {
	return &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		status, body := handler(req)
		var data []byte
		switch b := body.(type) {
		case string:
			data = []byte(b)
		default:
			data, _ = json.Marshal(b)
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(data)),
			Request:    req,
		}, nil
	})}
}

func TestFindAlbumByTitle(t *testing.T) {
	t.Run("found case-insensitively", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusOK, albumsListResponse{Albums: []albumEntry{
				{ID: "alb-1", Title: "Other"},
				{ID: "alb-2", Title: "DAYCARE photos"},
			}}
		})
		id, err := findAlbumByTitle(context.Background(), client, "Daycare Photos")
		if err != nil {
			t.Fatalf("findAlbumByTitle: %v", err)
		}
		if id != "alb-2" {
			t.Errorf("id = %q, want alb-2", id)
		}
	})

	t.Run("paginates", func(t *testing.T) {
		var tokens []string
		client := stubClient(func(req *http.Request) (int, any) {
			tokens = append(tokens, req.URL.Query().Get("pageToken"))
			if len(tokens) == 1 {
				return http.StatusOK, albumsListResponse{
					Albums:        []albumEntry{{ID: "alb-1", Title: "Other"}},
					NextPageToken: "page-2",
				}
			}
			return http.StatusOK, albumsListResponse{Albums: []albumEntry{{ID: "alb-2", Title: "Target"}}}
		})
		id, err := findAlbumByTitle(context.Background(), client, "Target")
		if err != nil {
			t.Fatalf("findAlbumByTitle: %v", err)
		}
		if id != "alb-2" {
			t.Errorf("id = %q, want alb-2", id)
		}
		if len(tokens) != 2 || tokens[0] != "" || tokens[1] != "page-2" {
			t.Errorf("page tokens = %v, want [\"\" page-2]", tokens)
		}
	})

	t.Run("not found", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusOK, albumsListResponse{}
		})
		id, err := findAlbumByTitle(context.Background(), client, "Missing")
		if err != nil || id != "" {
			t.Errorf("got (%q, %v), want empty id and nil error", id, err)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusForbidden, "denied"
		})
		_, err := findAlbumByTitle(context.Background(), client, "X")
		if err == nil || !strings.Contains(err.Error(), "status 403") {
			t.Errorf("err = %v, want status 403", err)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusOK, "{broken"
		})
		_, err := findAlbumByTitle(context.Background(), client, "X")
		if err == nil || !strings.Contains(err.Error(), "parsing albums list") {
			t.Errorf("err = %v, want parsing albums list", err)
		}
	})
}

func TestCreateAlbum(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			var body createAlbumRequest
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decoding create body: %v", err)
			}
			if body.Album.Title != "New Album" {
				t.Errorf("title = %q, want New Album", body.Album.Title)
			}
			return http.StatusOK, albumEntry{ID: "alb-new", Title: "New Album"}
		})
		id, err := createAlbum(context.Background(), client, "New Album")
		if err != nil {
			t.Fatalf("createAlbum: %v", err)
		}
		if id != "alb-new" {
			t.Errorf("id = %q, want alb-new", id)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusBadRequest, "bad"
		})
		_, err := createAlbum(context.Background(), client, "X")
		if err == nil || !strings.Contains(err.Error(), "status 400") {
			t.Errorf("err = %v, want status 400", err)
		}
	})
}

func TestFindOrCreateAlbum(t *testing.T) {
	t.Run("existing album returned", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusOK, albumsListResponse{Albums: []albumEntry{{ID: "alb-1", Title: "Existing"}}}
		})
		id, err := findOrCreateAlbum(context.Background(), client, "Existing")
		if err != nil || id != "alb-1" {
			t.Errorf("got (%q, %v), want alb-1", id, err)
		}
	})

	t.Run("created when missing", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			if req.Method == http.MethodGet {
				return http.StatusOK, albumsListResponse{}
			}
			return http.StatusOK, albumEntry{ID: "alb-created"}
		})
		id, err := findOrCreateAlbum(context.Background(), client, "Fresh")
		if err != nil || id != "alb-created" {
			t.Errorf("got (%q, %v), want alb-created", id, err)
		}
	})

	t.Run("search error propagates", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusForbidden, "denied"
		})
		_, err := findOrCreateAlbum(context.Background(), client, "X")
		if err == nil || !strings.Contains(err.Error(), "searching for album") {
			t.Errorf("err = %v, want searching for album", err)
		}
	})

	t.Run("create error propagates", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			if req.Method == http.MethodGet {
				return http.StatusOK, albumsListResponse{}
			}
			return http.StatusBadRequest, "bad"
		})
		_, err := findOrCreateAlbum(context.Background(), client, "X")
		if err == nil || !strings.Contains(err.Error(), "creating album") {
			t.Errorf("err = %v, want creating album", err)
		}
	})
}

func TestListUploadedAttachmentIDs(t *testing.T) {
	start := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC)

	t.Run("extracts ids and applies date buffer", func(t *testing.T) {
		var pages []searchMediaItemsRequest
		client := stubClient(func(req *http.Request) (int, any) {
			var body searchMediaItemsRequest
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decoding search body: %v", err)
			}
			pages = append(pages, body)
			if len(pages) == 1 {
				return http.StatusOK, searchMediaItemsResponse{
					MediaItems: []mediaItemEntry{
						{ID: "m1", Filename: "daycare_2024-01-15_0123456789abcdef01234567.jpg"},
						{ID: "m2", Filename: "vacation.jpg"},
					},
					NextPageToken: "page-2",
				}
			}
			return http.StatusOK, searchMediaItemsResponse{
				MediaItems: []mediaItemEntry{
					{ID: "m3", Filename: "daycare_2024-01-16_abcdefabcdefabcdefabcdef.jpg"},
					{ID: "m4", Filename: "daycare_2024-01-16_short.jpg"},
					{ID: "m5", Filename: "daycare_2024-01-17_fedcbafedcbafedcbafedcba.mp4"},
				},
			}
		})

		ids, err := listUploadedAttachmentIDs(context.Background(), client, start, end)
		if err != nil {
			t.Fatalf("listUploadedAttachmentIDs: %v", err)
		}
		if len(ids) != 3 || !ids["0123456789abcdef01234567"] || !ids["abcdefabcdefabcdefabcdef"] || !ids["fedcbafedcbafedcbafedcba"] {
			t.Errorf("ids = %v", ids)
		}

		// The query window must be expanded by uploadedIDsDateBuffer days.
		r := pages[0].Filters.DateFilter.Ranges[0]
		if r.StartDate.Day != 8 || r.EndDate.Day != 22 {
			t.Errorf("buffered range = %+v, want days 8..22", r)
		}
		if pages[1].PageToken != "page-2" {
			t.Errorf("second page token = %q, want page-2", pages[1].PageToken)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusForbidden, "denied"
		})
		_, err := listUploadedAttachmentIDs(context.Background(), client, start, end)
		if err == nil || !strings.Contains(err.Error(), "status 403") {
			t.Errorf("err = %v, want status 403", err)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusOK, "{broken"
		})
		_, err := listUploadedAttachmentIDs(context.Background(), client, start, end)
		if err == nil || !strings.Contains(err.Error(), "parsing search response") {
			t.Errorf("err = %v, want parsing search response", err)
		}
	})
}

func TestUploadBytes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			if got := req.Header.Get("X-Goog-Upload-File-Name"); got != "photo.jpg" {
				t.Errorf("filename header = %q", got)
			}
			if got := req.Header.Get("X-Goog-Upload-Protocol"); got != "raw" {
				t.Errorf("protocol header = %q", got)
			}
			data, _ := io.ReadAll(req.Body)
			if string(data) != "jpeg-data" {
				t.Errorf("body = %q", data)
			}
			return http.StatusOK, "upload-token-1"
		})
		token, err := uploadBytes(context.Background(), client, []byte("jpeg-data"), "photo.jpg")
		if err != nil || token != "upload-token-1" {
			t.Errorf("got (%q, %v), want upload-token-1", token, err)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusBadRequest, "bad"
		})
		_, err := uploadBytes(context.Background(), client, []byte("x"), "photo.jpg")
		if err == nil || !strings.Contains(err.Error(), "status 400") {
			t.Errorf("err = %v, want status 400", err)
		}
	})
}

func TestCreateMediaItem(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			var body batchCreateRequest
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decoding body: %v", err)
			}
			if body.AlbumID != "alb-1" || body.NewMediaItems[0].SimpleMediaItem.UploadToken != "tok-1" {
				t.Errorf("body = %+v", body)
			}
			return http.StatusOK, batchCreateResponse{NewMediaItemResults: []mediaItemResult{
				{UploadToken: "tok-1", MediaItem: mediaItem{ID: "media-1"}},
			}}
		})
		id, err := createMediaItem(context.Background(), client, "tok-1", "photo.jpg", "alb-1")
		if err != nil || id != "media-1" {
			t.Errorf("got (%q, %v), want media-1", id, err)
		}
	})

	t.Run("api status error", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusOK, batchCreateResponse{NewMediaItemResults: []mediaItemResult{
				{Status: apiStatus{Message: "quota exceeded", Code: 8}},
			}}
		})
		_, err := createMediaItem(context.Background(), client, "tok-1", "photo.jpg", "alb-1")
		if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
			t.Errorf("err = %v, want quota exceeded", err)
		}
	})

	t.Run("empty results", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusOK, batchCreateResponse{}
		})
		_, err := createMediaItem(context.Background(), client, "tok-1", "photo.jpg", "alb-1")
		if err == nil || !strings.Contains(err.Error(), "no media item results") {
			t.Errorf("err = %v, want no media item results", err)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusBadRequest, "bad"
		})
		_, err := createMediaItem(context.Background(), client, "tok-1", "photo.jpg", "alb-1")
		if err == nil || !strings.Contains(err.Error(), "status 400") {
			t.Errorf("err = %v, want status 400", err)
		}
	})
}

func TestUploadToPhotos(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			if strings.HasSuffix(req.URL.Path, "/uploads") {
				return http.StatusOK, "tok-1"
			}
			return http.StatusOK, batchCreateResponse{NewMediaItemResults: []mediaItemResult{
				{MediaItem: mediaItem{ID: "media-1"}},
			}}
		})
		id, err := uploadToPhotos(context.Background(), client, []byte("data"), "photo.jpg", "alb-1")
		if err != nil || id != "media-1" {
			t.Errorf("got (%q, %v), want media-1", id, err)
		}
	})

	t.Run("upload failure", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			return http.StatusBadRequest, "bad"
		})
		_, err := uploadToPhotos(context.Background(), client, []byte("data"), "photo.jpg", "alb-1")
		if err == nil || !strings.Contains(err.Error(), "uploading bytes") {
			t.Errorf("err = %v, want uploading bytes", err)
		}
	})

	t.Run("create failure", func(t *testing.T) {
		client := stubClient(func(req *http.Request) (int, any) {
			if strings.HasSuffix(req.URL.Path, "/uploads") {
				return http.StatusOK, "tok-1"
			}
			return http.StatusBadRequest, "bad"
		})
		_, err := uploadToPhotos(context.Background(), client, []byte("data"), "photo.jpg", "alb-1")
		if err == nil || !strings.Contains(err.Error(), "creating media item") {
			t.Errorf("err = %v, want creating media item", err)
		}
	})
}

func TestErrorBody(t *testing.T) {
	short := "short error"
	long := strings.Repeat("x", 300)

	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })

	// At INFO, long bodies are truncated.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if got := errorBody([]byte(short)); got != short {
		t.Errorf("short body = %q, want verbatim", got)
	}
	got := errorBody([]byte(long))
	if !strings.HasSuffix(got, "...(truncated)") || len(got) >= len(long) {
		t.Errorf("long body at INFO = %q, want truncated", got)
	}

	// At DEBUG, the full body is preserved.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if got := errorBody([]byte(long)); got != long {
		t.Errorf("long body at DEBUG truncated, want full body")
	}
}
