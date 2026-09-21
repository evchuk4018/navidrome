package personalradio

import (
	"context"
	"math"
	"testing"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

func TestBuildRadioContextIncludesStartedCurrentAndPlaylistSeeds(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "original", Title: "Original", Artist: "A", MbzRecordingID: "orig-mbid"},
		{ID: "playlist-2", Title: "Playlist Two", Artist: "B", MbzRecordingID: "two-mbid"},
		{ID: "current", Title: "Current", Artist: "C", MbzRecordingID: "current-mbid"},
	})
	repo := &fakePersonalRadioRepository{items: []model.PersonalRadioItem{
		{ID: "seed", ItemType: model.RadioItemSeed, Status: model.RadioItemReady, MediaFileID: "original"},
		{ID: "current-item", ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, MediaFileID: "current", PlaybackOutcome: model.RadioPlaybackStarted},
	}}
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: mediaRepo}, repo: repo}
	radioContext, err := svc.buildRadioContext(context.Background(), model.PersonalRadioSession{
		ID: "session", UserID: "user", SeedMediaFileID: "original", SeedMediaFileIDs: []string{"original", "playlist-2"},
	}, repo.items, model.RefillPersonalRadioRequest{CurrentItemID: "current-item"})
	if err != nil {
		t.Fatal(err)
	}
	weights := map[string]float64{}
	roles := map[string]string{}
	for _, seed := range radioContext.Seeds {
		weights[seed.File.ID] = seed.Weight
		roles[seed.File.ID] = seed.Role
	}
	if roles["current"] != "current_started" || weights["current"] <= 0 {
		t.Fatalf("started current track was not included at low confidence: %#v", radioContext.Seeds)
	}
	if roles["playlist-2"] != "playlist_seed" || weights["playlist-2"] <= 0 {
		t.Fatalf("playlist seed was not included: %#v", radioContext.Seeds)
	}
}

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
