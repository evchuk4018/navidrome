package persistence

import (
	"testing"
	"time"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

func TestTasteEvidenceSaturatesPositiveHistory(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	identity := recommendations.TasteIdentityForMediaFile("", model.MediaFile{
		ID: "track-1", Artist: "Artist", Album: "Album", Genre: "Rock",
	})
	one := map[string]*tasteEvidence{}
	addMediaEvidence(one, identity, 1, 0, now, now)
	many := map[string]*tasteEvidence{}
	for i := 0; i < 10; i++ {
		addMediaEvidence(many, identity, 1, 0, now, now)
	}
	if many["track\x00media:track-1"].score() <= one["track\x00media:track-1"].score() {
		t.Fatalf("repeated track evidence did not increase score: one=%v many=%v", one["track\x00media:track-1"].score(), many["track\x00media:track-1"].score())
	}
	if many["track\x00media:track-1"].score() > 1 {
		t.Fatalf("taste score escaped bounds: %v", many["track\x00media:track-1"].score())
	}
}

func TestTasteEvidenceDecayAndNegativeFeedback(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	identity := recommendations.TasteIdentityForMediaFile("", model.MediaFile{ID: "track-1", Artist: "Artist"})
	fresh := map[string]*tasteEvidence{}
	addMediaEvidence(fresh, identity, 3, 0, now, now)
	old := map[string]*tasteEvidence{}
	addMediaEvidence(old, identity, 3, 0, now.Add(-240*24*time.Hour), now)
	if old["track\x00media:track-1"].score() >= fresh["track\x00media:track-1"].score() {
		t.Fatalf("old evidence score=%v, fresh=%v; want old lower", old["track\x00media:track-1"].score(), fresh["track\x00media:track-1"].score())
	}
	negative := map[string]*tasteEvidence{}
	addMediaEvidence(negative, identity, 0, 5, now, now)
	if got := negative["track\x00media:track-1"].score(); got != 0 {
		t.Fatalf("negative-only evidence score=%v, want 0", got)
	}
}

func TestTasteIdentityUsesTextFallbackWithoutMediaID(t *testing.T) {
	identity := recommendations.TasteIdentityForMediaFile("", model.MediaFile{Title: "  Song  ", Artist: "  Artist "})
	if identity.TrackKey != "track:title:song|artist:artist" {
		t.Fatalf("textual track key = %q", identity.TrackKey)
	}
}
