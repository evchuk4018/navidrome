package music

import (
	"testing"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

func TestRankExternalSearchKeepsMatchTiersAheadOfPopularity(t *testing.T) {
	input := model.ExternalMusicSearch{Songs: []model.ExternalTrack{
		{ID: "fuzzy", Title: "Blinding Light", ArtistName: "Cover Artist", Popularity: 1000000, ProviderScore: 1},
		{ID: "exact", Title: "Blinding Lights", ArtistName: "The Weeknd", Popularity: 1, ProviderScore: .5},
	}}
	result := rankExternalSearch("Blinding Lights", input, 30, nil)
	if len(result.Results) != 2 || result.Results[0].Song == nil || result.Results[0].Song.ID != "exact" {
		t.Fatalf("exact result should win its tier: %#v", result.Results)
	}
}

func TestRankExternalSearchExactCompositeWinsOverall(t *testing.T) {
	input := model.ExternalMusicSearch{
		Artists: []model.ExternalArtist{{ID: "artist", Name: "Hello", Popularity: 100}},
		Songs:   []model.ExternalTrack{{ID: "song", Title: "Hello", ArtistName: "Adele"}},
	}
	result := rankExternalSearch("Adele Hello", input, 30, nil)
	if result.Results[0].Song == nil || result.Results[0].Song.ID != "song" {
		t.Fatalf("composite recording should rank first: %#v", result.Results)
	}
}

func TestRankExternalSearchAffinityOnlyBreaksSameTier(t *testing.T) {
	input := model.ExternalMusicSearch{Artists: []model.ExternalArtist{
		{ID: "a", Name: "Shape of You", ProviderScore: .8},
		{ID: "b", Name: "Shape of You", ProviderScore: .8},
	}}
	affinities := map[string]recommendations.TasteAffinity{
		"artist:b": recommendations.ComposeTasteAffinity(0, 1, 0, 0),
	}
	result := rankExternalSearch("Shape of You", input, 30, affinities)
	if result.Results[0].Artist == nil || result.Results[0].Artist.ID != "b" {
		t.Fatalf("affinity should resolve a same-tier tie: %#v", result.Results)
	}
}

func TestRankExternalSearchPreservesVersionsDuringDeduplication(t *testing.T) {
	input := model.ExternalMusicSearch{Songs: []model.ExternalTrack{
		{ID: "studio-a", Title: "Hello", ArtistName: "Adele", Duration: 295, ISRCs: []string{"GB-A-1"}},
		{ID: "studio-b", Title: "Hello", ArtistName: "Adele", Duration: 296, ISRCs: []string{"GB-A-1"}, Popularity: 4},
		{ID: "live", Title: "Hello (Live)", ArtistName: "Adele", Duration: 300, Version: "live"},
	}}
	result := rankExternalSearch("Hello", input, 30, nil)
	if len(result.Songs) != 2 {
		t.Fatalf("expected equivalent studio recordings merged and live preserved: %#v", result.Songs)
	}
}
