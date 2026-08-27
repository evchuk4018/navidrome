package personalradio

import (
	"math"
	"testing"

	"github.com/navidrome/navidrome/model"
)

func TestWeightedRadioSeedsNormalizeAndRetainOriginalContext(t *testing.T) {
	original := &model.MediaFile{ID: "original", MbzRecordingID: "same-mbid"}
	current := &model.MediaFile{ID: "current", MbzRecordingID: "current-mbid"}
	accepted := &model.MediaFile{ID: "accepted", MbzRecordingID: "accepted-mbid"}
	seeds := weightedRadioSeeds([]radioSeedInput{
		{file: original, weight: 0.35, role: "original"},
		{file: current, weight: 0.30, role: "current"},
		{file: accepted, weight: 0.18, role: "accepted_recent_1"},
	})
	if len(seeds) != 3 {
		t.Fatalf("got %d session seeds, want 3", len(seeds))
	}
	var total float64
	for _, seed := range seeds {
		total += seed.Weight
	}
	if math.Abs(total-1) > 1e-9 {
		t.Fatalf("normalized seed weights sum to %v, want 1", total)
	}
	if seeds[0].Role != "original" || seeds[0].Weight <= seeds[1].Weight {
		t.Fatalf("original seed = %#v, want first and weighted above current", seeds[0])
	}

	merged := weightedRadioSeeds([]radioSeedInput{
		{file: original, weight: 0.35, role: "original"},
		{file: &model.MediaFile{ID: "alias", MbzRecordingID: "SAME-MBID"}, weight: 0.30, role: "current"},
	})
	if len(merged) != 1 || math.Abs(merged[0].Weight-1) > 1e-9 {
		t.Fatalf("duplicate stable identity was not merged: %#v", merged)
	}
}

func TestRadioOutstandingItemsHonorsClientQueueReconciliation(t *testing.T) {
	items := []model.PersonalRadioItem{
		{ID: "current", ItemType: model.RadioItemLibrary, Status: model.RadioItemPlayed},
		{ID: "queued", ItemType: model.RadioItemDiscovery, Status: model.RadioItemDownloading},
		{ID: "stale", ItemType: model.RadioItemLibrary, Status: model.RadioItemReady},
	}
	context := &radioContext{
		ClientQueueProvided: true,
		CurrentItemID:       "current",
		QueuedItemIDs:       map[string]bool{"current": true, "queued": true},
	}
	if got := radioOutstandingItems(items, context); got != 2 {
		t.Fatalf("client queue outstanding count = %d, want 2", got)
	}
	if got := radioOutstandingItems(items, nil); got != 2 {
		t.Fatalf("server queue outstanding count = %d, want 2", got)
	}
}
