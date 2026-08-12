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
