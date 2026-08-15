package musicbrainz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecordingMapsMusicBrainzMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/recording/11111111-1111-4111-8111-111111111111" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if r.URL.Query().Get("fmt") != "json" {
			t.Fatalf("expected JSON response, got query %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "id": "11111111-1111-4111-8111-111111111111",
            "title": "Song",
            "length": 215000,
            "artist-credit": [{"name": "Artist", "artist": {"id": "22222222-2222-4222-8222-222222222222", "name": "Artist"}}],
            "releases": [{
                "date": "2024-05-01",
                "release-group": {
                    "id": "33333333-3333-4333-8333-333333333333",
                    "title": "Album",
                    "first-release-date": "2024-05-01"
                }
            }]
        }`))
	}))
	defer server.Close()

	client := NewWithClient(server.URL, server.Client())
	track, err := client.Recording(context.Background(), "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("Recording returned error: %v", err)
	}
	if track.Title != "Song" || track.ArtistName != "Artist" || track.AlbumTitle != "Album" {
		t.Fatalf("unexpected track metadata: %#v", track)
	}
	if track.Duration != 215 || track.Year != 2024 || track.AlbumID == "" {
		t.Fatalf("unexpected duration/year/album: %#v", track)
	}
}

func TestRecordingRejectsNonMusicBrainzID(t *testing.T) {
	client := NewWithClient("http://127.0.0.1:1", http.DefaultClient)
	if _, err := client.Recording(context.Background(), "not-an-id"); err == nil {
		t.Fatal("expected invalid ID error")
	}
}

func TestSearchSongsReturnsRankedRecordings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/recording" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "recordings": [
                {"id": "11111111-1111-4111-8111-111111111111", "title": "Song", "artist-credit": [{"name": "Artist"}]},
                {"id": "22222222-2222-4222-8222-222222222222", "title": "Other", "artist-credit": [{"name": "Someone Else"}]}
            ]
        }`))
	}))
	defer server.Close()

	client := NewWithClient(server.URL, server.Client())
	songs, err := client.SearchSongs(context.Background(), "Artist Song")
	if err != nil {
		t.Fatalf("SearchSongs returned error: %v", err)
	}
	if len(songs) != 2 {
		t.Fatalf("expected two songs, got %#v", songs)
	}
	if songs[0].ID != "11111111-1111-4111-8111-111111111111" || songs[0].Title != "Song" || songs[0].ArtistName != "Artist" {
		t.Fatalf("unexpected first song: %#v", songs[0])
	}
}

func TestGetRetriesTransientResponses(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": "11111111-1111-4111-8111-111111111111", "title": "Song"}`))
	}))
	defer server.Close()

	client := NewWithClient(server.URL, server.Client())
	track, err := client.Recording(context.Background(), "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("Recording returned error: %v", err)
	}
	if track.Title != "Song" {
		t.Fatalf("unexpected track after retry: %#v", track)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
}

func TestGetFailsAfterRepeatedTransientResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewWithClient(server.URL, server.Client())
	if _, err := client.Recording(context.Background(), "11111111-1111-4111-8111-111111111111"); err == nil {
		t.Fatal("expected error after repeated transient responses")
	}
}
