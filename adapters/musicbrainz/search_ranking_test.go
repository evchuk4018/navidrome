package musicbrainz

import (
	"testing"

	"github.com/navidrome/navidrome/model"
)

func TestSortRecordingsByRelevancePrioritizesExactAndPhraseTitleMatches(t *testing.T) {
	recordings := []mbRecording{
		{
			ID:    "broad-match",
			Title: "Teenage Dream",
			ArtistCredit: []mbArtistCredit{{
				Name: "Katy Perry",
			}},
		},
		{
			ID:    "phrase-match",
			Title: "The One That Got Away (Live)",
		},
		{
			ID:    "exact-match",
			Title: "The One That Got Away",
			ArtistCredit: []mbArtistCredit{{
				Name: "Katy Perry",
			}},
		},
	}

	sortRecordingsByRelevance(recordings, "The One That Got Away")

	if got := []string{recordings[0].ID, recordings[1].ID, recordings[2].ID}; got[0] != "exact-match" || got[1] != "phrase-match" || got[2] != "broad-match" {
		t.Fatalf("unexpected recording order: %v", got)
	}
}

func TestSortRecordingsByRelevancePrioritizesArtistMatchesAfterTitleMatches(t *testing.T) {
	recordings := []mbRecording{
		{
			ID:    "other",
			Title: "Roar",
		},
		{
			ID:    "artist-match",
			Title: "Teenage Dream",
			ArtistCredit: []mbArtistCredit{{
				Name: "Katy Perry",
			}},
		},
	}

	sortRecordingsByRelevance(recordings, "Katy Perry")

	if recordings[0].ID != "artist-match" {
		t.Fatalf("expected artist match first, got %q", recordings[0].ID)
	}
}

func TestNormalizeSearchTextIgnoresPunctuationAndCase(t *testing.T) {
	if got := normalizeSearchText("  The One-That Got Away! "); got != "the one that got away" {
		t.Fatalf("unexpected normalized text %q", got)
	}
}

func TestSortRecordingsByRelevanceUsesPopularityWithinTitleMatches(t *testing.T) {
	recordings := []mbRecording{
		{ID: "obscure", Title: "The One That Got Away", Score: 100},
		{ID: "katy", Title: "The One That Got Away", Score: 100},
		{ID: "unrelated", Title: "Teenage Dream", Score: 100},
	}

	sortRecordingsByRelevance(recordings, "The One That Got Away", map[string]model.RecordingPopularity{
		"obscure": {TotalListenCount: 20, TotalUserCount: 2},
		"katy":    {TotalListenCount: 1_800_000, TotalUserCount: 500_000},
		"unrelated": {
			TotalListenCount: 9_000_000,
			TotalUserCount:   2_000_000,
		},
	})

	if got := []string{recordings[0].ID, recordings[1].ID, recordings[2].ID}; got[0] != "katy" || got[1] != "obscure" || got[2] != "unrelated" {
		t.Fatalf("unexpected recording order: %v", got)
	}
}

func TestSortRecordingsByRelevanceFallsBackToMusicBrainzScore(t *testing.T) {
	recordings := []mbRecording{
		{ID: "lower-score", Title: "Song", Score: 80},
		{ID: "higher-score", Title: "Song", Score: 100},
	}

	sortRecordingsByRelevance(recordings, "Song")

	if recordings[0].ID != "higher-score" {
		t.Fatalf("expected higher MusicBrainz score first, got %q", recordings[0].ID)
	}
}
