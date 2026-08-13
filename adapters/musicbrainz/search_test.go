package musicbrainz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/navidrome/navidrome/model"
)

type fakePopularityProvider struct {
	popularity map[string]model.RecordingPopularity
	err        error
	requested  []string
}

func (f *fakePopularityProvider) GetRecordingPopularity(_ context.Context, recordingIDs []string) (map[string]model.RecordingPopularity, error) {
	f.requested = append([]string(nil), recordingIDs...)
	return f.popularity, f.err
}

func newSearchFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/artist":
			_, _ = w.Write([]byte(`{"artists":[]}`))
		case "/release-group":
			_, _ = w.Write([]byte(`{"release-groups":[]}`))
		case "/recording":
			if got := r.URL.Query().Get("limit"); got != "100" {
				t.Errorf("expected recording search limit 100, got %q", got)
			}
			_, _ = w.Write([]byte(`{
                "recordings": [
                    {"id":"obscure","title":"The One That Got Away","score":100,"artist-credit":[{"name":"Obscure Artist"}]},
					{"id":"katy","title":"The One That Got Away","score":100,"artist-credit":[{"name":"Katy Perry"}],"releases":[{"title":"Teenage Dream","date":"2010-08-24","release-group":{"id":"album-id","title":"Teenage Dream","first-release-date":"2010-08-24"}}]},
                    {"id":"unrelated","title":"Teenage Dream","score":100,"artist-credit":[{"name":"Katy Perry"}]}
                ]
            }`))
		case "/tag":
			_, _ = w.Write([]byte(`{"tags":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSearchRanksRecordingsByListenBrainzPopularity(t *testing.T) {
	server := newSearchFixtureServer(t)
	defer server.Close()

	popularity := &fakePopularityProvider{popularity: map[string]model.RecordingPopularity{
		"obscure": {TotalListenCount: 20, TotalUserCount: 2},
		"katy":    {TotalListenCount: 1_800_000, TotalUserCount: 500_000},
		"unrelated": {
			TotalListenCount: 9_000_000,
			TotalUserCount:   2_000_000,
		},
	}}
	client := newWithPopularity(server.URL, server.Client(), popularity)

	result, err := client.Search(context.Background(), "The One That Got Away")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := []string{result.Songs[0].ID, result.Songs[1].ID, result.Songs[2].ID}; got[0] != "katy" || got[1] != "obscure" || got[2] != "unrelated" {
		t.Fatalf("unexpected song order: %v", got)
	}
	if got := popularity.requested; len(got) != 3 || got[0] != "obscure" || got[1] != "katy" || got[2] != "unrelated" {
		t.Fatalf("unexpected popularity request IDs: %v", got)
	}
	if got := result.Songs[0]; got.AlbumID != "album-id" || got.AlbumTitle != "Teenage Dream" || got.Year != 2010 || got.ImageURL != "https://coverartarchive.org/release-group/album-id/front-250" {
		t.Fatalf("expected release artwork metadata on song result, got %+v", got)
	}
}

func TestSearchFallsBackWhenListenBrainzIsUnavailable(t *testing.T) {
	server := newSearchFixtureServer(t)
	defer server.Close()

	client := newWithPopularity(server.URL, server.Client(), &fakePopularityProvider{err: errors.New("ListenBrainz unavailable")})
	result, err := client.Search(context.Background(), "The One That Got Away")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(result.Songs) != 3 {
		t.Fatalf("expected all MusicBrainz results after popularity failure, got %d", len(result.Songs))
	}
}
