package musicbrainz

import "testing"

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
