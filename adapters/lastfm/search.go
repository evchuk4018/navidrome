package lastfm

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/conf"
)

type SearchSeed struct {
	Kind       string
	MBID       string
	Name       string
	Artist     string
	ImageURL   string
	Popularity float64
}

type CandidateSeeds struct {
	Seeds    []SearchSeed
	Degraded []string
}

type SearchClient struct{ client *client }

func NewSearchClient() *SearchClient {
	if !conf.Server.LastFM.Enabled || strings.TrimSpace(conf.Server.LastFM.ApiKey) == "" {
		return &SearchClient{}
	}
	return &SearchClient{client: newClient(conf.Server.LastFM.ApiKey, "", http.DefaultClient)}
}

func (c *SearchClient) Search(ctx context.Context, query string) CandidateSeeds {
	if c == nil || c.client == nil || strings.TrimSpace(query) == "" {
		return CandidateSeeds{}
	}
	type lane struct {
		kind     string
		response *Response
		err      error
	}
	results := make(chan lane, 3)
	for _, kind := range []string{"track", "artist", "album"} {
		go func(kind string) {
			params := url.Values{"method": {kind + ".search"}, kind: {query}, "limit": {"10"}}
			response, err := c.client.makeRequest(ctx, http.MethodGet, params, false)
			results <- lane{kind: kind, response: response, err: err}
		}(kind)
	}
	output := CandidateSeeds{Seeds: make([]SearchSeed, 0, 30)}
	for range 3 {
		result := <-results
		if result.err != nil {
			output.Degraded = append(output.Degraded, "lastfm:"+result.kind)
			continue
		}
		switch result.kind {
		case "track":
			for _, item := range result.response.Results.TrackMatches.Tracks {
				output.Seeds = append(output.Seeds, SearchSeed{Kind: "song", MBID: item.MBID, Name: item.Name, Artist: item.Artist, ImageURL: trustedImage(item.Image), Popularity: listeners(item.Listeners)})
			}
		case "artist":
			for _, item := range result.response.Results.ArtistMatches.Artists {
				output.Seeds = append(output.Seeds, SearchSeed{Kind: "artist", MBID: item.MBID, Name: item.Name, ImageURL: trustedImage(item.Image), Popularity: listeners(item.Listeners)})
			}
		case "album":
			for _, item := range result.response.Results.AlbumMatches.Albums {
				output.Seeds = append(output.Seeds, SearchSeed{Kind: "album", MBID: item.MBID, Name: item.Name, Artist: item.Artist, ImageURL: trustedImage(item.Image)})
			}
		}
	}
	return output
}

func trustedImage(images []ExternalImage) string {
	for index := len(images) - 1; index >= 0; index-- {
		if strings.HasPrefix(strings.ToLower(images[index].URL), "https://") {
			return images[index].URL
		}
	}
	return ""
}

func listeners(value string) float64 { parsed, _ := strconv.ParseFloat(value, 64); return parsed }
