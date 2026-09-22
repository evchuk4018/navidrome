package lastfm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
)

type searchHTTPDoer struct{ malformed bool }

func (d searchHTTPDoer) Do(request *http.Request) (*http.Response, error) {
	body := `{}`
	if d.malformed {
		body = `{`
	} else {
		switch request.URL.Query().Get("method") {
		case "track.search":
			body = `{"results":{"trackmatches":{"track":[{"name":"Hello","artist":"Adele","mbid":"","listeners":"123","image":[{"#text":"http://bad/image","size":"small"},{"#text":"https://last.fm/hello.jpg","size":"large"}]}]}}}`
		case "artist.search":
			body = `{"results":{"artistmatches":{"artist":[{"name":"Adele","mbid":"artist-id","listeners":"456"}]}}}`
		case "album.search":
			body = `{"results":{"albummatches":{"album":[{"name":"25","artist":"Adele","mbid":"album-id"}]}}}`
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
}

func TestSearchReturnsPopularSeedsWithoutRequiringMBID(t *testing.T) {
	client := &SearchClient{client: newClient("key", "", searchHTTPDoer{})}
	result := client.Search(context.Background(), "Adele Hello")
	if len(result.Degraded) != 0 || len(result.Seeds) != 3 {
		t.Fatalf("unexpected search result: %#v", result)
	}
	var track SearchSeed
	for _, seed := range result.Seeds {
		if seed.Kind == "song" {
			track = seed
		}
	}
	if track.Name != "Hello" || track.MBID != "" || track.Popularity != 123 || track.ImageURL != "https://last.fm/hello.jpg" {
		t.Fatalf("unexpected track seed: %#v", track)
	}
}

func TestSearchDegradesMalformedLanes(t *testing.T) {
	client := &SearchClient{client: newClient("key", "", searchHTTPDoer{malformed: true})}
	result := client.Search(context.Background(), "Hello")
	if len(result.Seeds) != 0 || len(result.Degraded) != 3 {
		t.Fatalf("unexpected malformed result: %#v", result)
	}
}
