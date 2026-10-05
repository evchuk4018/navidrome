package musicbrainz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
		inc := r.URL.Query().Get("inc")
		if !strings.Contains(inc, "genres") {
			t.Fatalf("expected typed genres include, got query %q", r.URL.RawQuery)
		}
		if !strings.Contains(inc, "release-groups") {
			t.Fatalf("expected release-groups include for nested group metadata, got query %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "id": "11111111-1111-4111-8111-111111111111",
            "title": "Song",
            "length": 215000,
            "artist-credit": [{"name": "Artist", "artist": {"id": "22222222-2222-4222-8222-222222222222", "name": "Artist"}}],
            "genres": [{"name": "Hip-Hop", "count": 2}],
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
	if track.Duration != 215 || track.Year != 2024 || track.AlbumID != "33333333-3333-4333-8333-333333333333" || track.Genre != "Hip-Hop" {
		t.Fatalf("unexpected duration/year/album: %#v", track)
	}
}

func TestRecordingRejectsNonMusicBrainzID(t *testing.T) {
	client := NewWithClient("http://127.0.0.1:1", http.DefaultClient)
	if _, err := client.Recording(context.Background(), "not-an-id"); err == nil {
		t.Fatal("expected invalid ID error")
	}
}

func TestRecordingGenreResolutionUsesReleaseGroupBeforeArtist(t *testing.T) {
	t.Run("selected release group", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/recording/11111111-1111-4111-8111-111111111111":
				inc := r.URL.Query().Get("inc")
				if !strings.Contains(inc, "genres") || !strings.Contains(inc, "release-groups") {
					t.Fatalf("recording lookup did not request typed group genres: %q", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{
                "id": "11111111-1111-4111-8111-111111111111",
                "title": "Song",
                "artist-credit": [{"name": "Artist", "artist": {"id": "22222222-2222-4222-8222-222222222222", "name": "Artist"}}],
                "releases": [
                    {"date": "2024-05-01", "status": "Official", "release-group": {"id": "33333333-3333-4333-8333-333333333333", "title": "Album"}},
                    {"date": "2020-05-01", "status": "Bootleg", "release-group": {"id": "44444444-4444-4444-8444-444444444444", "title": "Bootleg"}}
                ]
				}`))
			case "/release-group/33333333-3333-4333-8333-333333333333":
				if r.URL.Query().Get("inc") != "genres" {
					t.Fatalf("selected release-group lookup did not request only genres: %q", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{"id":"33333333-3333-4333-8333-333333333333","genres":[{"name":"Rock","count":2}]}`))
			default:
				t.Fatalf("unexpected request path %q", r.URL.Path)
			}
		}))
		defer server.Close()

		track, err := NewWithClient(server.URL, server.Client()).Recording(context.Background(), "11111111-1111-4111-8111-111111111111")
		if err != nil {
			t.Fatalf("Recording returned error: %v", err)
		}
		if track.Genre != "Rock" {
			t.Fatalf("genre = %q, want selected official release-group genre Rock", track.Genre)
		}
	})

	t.Run("primary artist", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/recording/11111111-1111-4111-8111-111111111111":
				inc := r.URL.Query().Get("inc")
				if !strings.Contains(inc, "genres") || !strings.Contains(inc, "release-groups") {
					t.Fatalf("recording lookup did not request typed group genres: %q", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{
                    "id": "11111111-1111-4111-8111-111111111111",
                    "title": "Song",
                    "artist-credit": [{"name": "Artist", "artist": {"id": "22222222-2222-4222-8222-222222222222", "name": "Artist"}}],
                    "releases": [{"date": "2024-05-01", "status": "Official", "release-group": {"id": "33333333-3333-4333-8333-333333333333", "title": "Album"}}]
                }`))
			case "/release-group/33333333-3333-4333-8333-333333333333":
				if r.URL.Query().Get("inc") != "genres" {
					t.Fatalf("release-group fallback did not request only genres: %q", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{"id":"33333333-3333-4333-8333-333333333333","genres":[]}`))
			case "/artist/22222222-2222-4222-8222-222222222222":
				if !strings.Contains(r.URL.Query().Get("inc"), "genres") {
					t.Fatalf("artist fallback did not request genres: %q", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{"id":"22222222-2222-4222-8222-222222222222","genres":[{"name":"Music","count":50},{"name":"Hip hop","count":4}]}`))
			default:
				t.Fatalf("unexpected request path %q", r.URL.Path)
			}
		}))
		defer server.Close()

		track, err := NewWithClient(server.URL, server.Client()).Recording(context.Background(), "11111111-1111-4111-8111-111111111111")
		if err != nil {
			t.Fatalf("Recording returned error: %v", err)
		}
		if track.Genre != "Hip hop" {
			t.Fatalf("genre = %q, want primary artist genre Hip hop", track.Genre)
		}
	})
}

func TestSupportedMusicBrainzGenreChoosesPositiveCountThenName(t *testing.T) {
	got := supportedMusicBrainzGenre([]mbGenre{
		{Name: "Music", Count: 100},
		{Name: "Pop Rap", Count: 2},
		{Name: "Chill Rap", Count: 2},
		{Name: "Trap", Count: 0},
	})
	if got != "Chill Rap" {
		t.Fatalf("supported genre = %q, want Chill Rap", got)
	}
}

func TestSupportedMusicBrainzGenreRejectsRenditionLabelsAndURLs(t *testing.T) {
	for _, genre := range []string{
		"slowed + reverb",
		"Slowed & Reverb",
		"Hip-Hop Remix",
		"guitar remix",
		"edit audio",
		"https://example.test/genre",
	} {
		if isSupportedMusicBrainzGenre(genre) {
			t.Fatalf("isSupportedMusicBrainzGenre(%q) = true, want false", genre)
		}
	}
	if !isSupportedMusicBrainzGenre("Electronic") {
		t.Fatal("isSupportedMusicBrainzGenre(Electronic) = false, want true")
	}
}

func TestSearchSongsReturnsRankedRecordings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/recording" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		inc := r.URL.Query().Get("inc")
		if !strings.Contains(inc, "release-groups") {
			t.Fatalf("expected search to include nested release groups, got query %q", r.URL.RawQuery)
		}
		if strings.Contains(inc, "genres") {
			t.Fatalf("search should not fetch genres for every result, got query %q", r.URL.RawQuery)
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
