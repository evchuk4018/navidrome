package quickpick

import (
	"context"
	"testing"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

type fakeMetrics struct {
	recent    map[string]int64
	playlists map[string]model.PlaylistPlayMetric
	exposures map[string]model.QuickPickExposureMetric
	recorded  map[string]int
	recordErr error
}

func (f fakeMetrics) SongRecentPlays(string, time.Time) (map[string]int64, error) {
	return f.recent, nil
}
func (f fakeMetrics) PlaylistMetrics(string, time.Time) (map[string]model.PlaylistPlayMetric, error) {
	return f.playlists, nil
}
func (f fakeMetrics) RecordPlaylistPlay(string, string, time.Time) error { return nil }
func (f fakeMetrics) ExposureMetrics(string, []string) (map[string]model.QuickPickExposureMetric, error) {
	return f.exposures, nil
}
func (f fakeMetrics) RecordExposures(_ string, itemKeys []string, _ time.Time) error {
	if f.recorded != nil {
		for _, key := range itemKeys {
			f.recorded[key]++
		}
	}
	return f.recordErr
}
func (f fakeMetrics) RecordImpressions(_ string, _ string, _ []string, _ time.Time) error {
	return f.recordErr
}

type fakeTasteMetrics struct {
	fakeMetrics
	affinity map[string]recommendations.TasteAffinity
}

func (f fakeTasteMetrics) Rebuild(string, time.Time) error     { return nil }
func (f fakeTasteMetrics) EnsureFresh(string, time.Time) error { return nil }
func (f fakeTasteMetrics) AffinityForCandidates(string, []recommendations.TasteCandidateIdentity) (map[string]recommendations.TasteAffinity, error) {
	return f.affinity, nil
}

type trackingMetrics struct {
	fakeMetrics
	exposures map[string]model.QuickPickExposureMetric
}

func (m *trackingMetrics) ExposureMetrics(_ string, itemKeys []string) (map[string]model.QuickPickExposureMetric, error) {
	result := make(map[string]model.QuickPickExposureMetric, len(itemKeys))
	for _, key := range itemKeys {
		if metric, ok := m.exposures[key]; ok {
			result[key] = metric
		}
	}
	return result, nil
}

func (m *trackingMetrics) RecordExposures(_ string, itemKeys []string, shownAt time.Time) error {
	if m.exposures == nil {
		m.exposures = map[string]model.QuickPickExposureMetric{}
	}
	for _, key := range itemKeys {
		metric := m.exposures[key]
		metric.ItemKey = key
		metric.ShowCount++
		metric.LastShownAt = shownAt
		m.exposures[key] = metric
	}
	return nil
}

func (m *trackingMetrics) RecordImpressions(_ string, _ string, itemKeys []string, shownAt time.Time) error {
	return m.RecordExposures("", itemKeys, shownAt)
}

type fakeSimilarityProvider struct {
	songs   []agents.Song
	bySeed  map[string][]agents.Song
	seedIDs *[]string
}

func (f fakeSimilarityProvider) GetSimilarSongsByTrackAll(_ context.Context, seedID, _, _, _ string, _ int) ([]agents.Song, error) {
	if f.seedIDs != nil {
		*f.seedIDs = append(*f.seedIDs, seedID)
	}
	if songs, ok := f.bySeed[seedID]; ok {
		return songs, nil
	}
	return f.songs, nil
}

func TestQuickPickKeepsTheRealFavoriteAtTheTop(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{{ID: "favorite", Title: "Favorite", Annotations: model.Annotations{PlayCount: 100}}}
	for i := 0; i < 8; i++ {
		files = append(files, model.MediaFile{ID: string(rune('a' + i)), Title: "Other", Annotations: model.Annotations{PlayCount: int64(i + 1)}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := New(ds, fakeMetrics{recent: map[string]int64{"favorite": 20}}, nil, nil)
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 9 {
		t.Fatalf("got %d items, want 9", len(response.Items))
	}
	if response.Items[0].Song == nil || response.Items[0].Song.ID != "favorite" {
		t.Fatalf("favorite was rotated out of the top position: %#v", response.Items[0])
	}
}

func TestQuickPickSurfacesLibraryMatchedRecommendations(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Genre: "Pop", Annotations: model.Annotations{PlayCount: 50}},
		{ID: "matched", Title: "Similar One", Artist: "Other Artist", Genre: "Pop", Annotations: model.Annotations{PlayCount: 1}},
	}
	for i := 0; i < 10; i++ {
		files = append(files, model.MediaFile{ID: string(rune('k' + i)), Title: "Filler", Annotations: model.Annotations{PlayCount: 2}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{songs: []agents.Song{
			{ID: "matched", Name: "Similar One", Artists: []agents.Artist{{Name: "Other Artist"}}},
		}},
		matcher: matcher.New(ds),
	}
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items {
		if item.Kind == model.QuickPickRecommendationKind {
			t.Fatalf("GET should not call similarity providers: %#v", item)
		}
	}
	foundMatched := false
	for _, item := range response.Items {
		if item.Song != nil && item.Song.ID == "matched" {
			foundMatched = true
		}
	}
	if !foundMatched {
		t.Fatalf("local radio pool should include matched library song: %#v", response.Items)
	}
}

func TestQuickPickOrdersMatchedRecommendationsBySimilarity(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Annotations: model.Annotations{PlayCount: 50}},
		{ID: "matched-low", Title: "Low Match", Artist: "Other Artist"},
		{ID: "matched-high", Title: "High Match", Artist: "Other Artist"},
	}
	for i := 0; i < 10; i++ {
		files = append(files, model.MediaFile{ID: string(rune('k' + i)), Title: "Filler", Annotations: model.Annotations{PlayCount: 2}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{bySeed: map[string][]agents.Song{"seed": {
			{ID: "matched-low", Name: "Low Match", Artists: []agents.Artist{{Name: "Other Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.2, NormalizedScore: 0.2}}},
			{ID: "matched-high", Name: "High Match", Artists: []agents.Artist{{Name: "Other Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.9, NormalizedScore: 0.9}}},
		}}},
		matcher: matcher.New(ds),
	}

	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items {
		if item.Kind == model.QuickPickRecommendationKind {
			t.Fatalf("GET should not call similarity providers: %#v", item)
		}
	}
}

func TestQuickPickKeepsDistinctMBIDRecommendationsWithSharedMetadata(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Annotations: model.Annotations{PlayCount: 50}},
		{ID: "library-a", Title: "Shared Track", Artist: "Shared Artist", MbzRecordingID: "recording-a"},
		{ID: "library-b", Title: "Shared Track", Artist: "Shared Artist", MbzRecordingID: "recording-b"},
	}
	for i := 0; i < 10; i++ {
		files = append(files, model.MediaFile{ID: string(rune('k' + i)), Title: "Filler", Annotations: model.Annotations{PlayCount: 2}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{bySeed: map[string][]agents.Song{"seed": {
			{Name: "Shared Track", MBID: "recording-a", Artists: []agents.Artist{{Name: "Shared Artist"}}, CandidateID: "mbid:recording-a"},
			{Name: "Shared Track", MBID: "recording-b", Artists: []agents.Artist{{Name: "Shared Artist"}}, CandidateID: "mbid:recording-b"},
		}}},
		matcher: matcher.New(ds),
	}

	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items {
		if item.Kind == model.QuickPickRecommendationKind {
			t.Fatalf("GET should not call similarity providers: %#v", item)
		}
	}
}

func TestQuickPickSkipsUnmatchedRecommendations(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	media.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Genre: "Pop", Annotations: model.Annotations{PlayCount: 50}},
	})
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{songs: []agents.Song{
			{ID: "not-in-library", Name: "Fresh Track", Artists: []agents.Artist{{Name: "New Artist"}}},
		}},
		matcher: matcher.New(ds),
	}
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items {
		if item.Kind == model.QuickPickRecommendationKind {
			t.Fatalf("unmatched recommendation should not be surfaced: %#v", item)
		}
	}
}

func TestQuickPickRecordsExposureFailureWithoutFailing(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{{ID: "anchor", Title: "Anchor", Annotations: model.Annotations{PlayCount: 100}}}
	for i := 0; i < 10; i++ {
		files = append(files, model.MediaFile{ID: string(rune('a' + i)), Title: "Filler", Annotations: model.Annotations{PlayCount: 2}})
	}
	media.SetData(files)
	recorded := map[string]int{}
	metrics := fakeMetrics{
		recent:   map[string]int64{"anchor": 20},
		recorded: recorded,
	}
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := New(ds, metrics, nil, nil)
	if _, err := svc.Get(context.Background(), "user"); err != nil {
		t.Fatalf("Get() returned exposure write error: %v", err)
	}
	if len(recorded) != 0 {
		t.Fatalf("GET recorded exposures = %#v, want none", recorded)
	}
}

func TestQuickPickExcludesMainSongsFromSmartPicks(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Annotations: model.Annotations{PlayCount: 100}},
		{ID: "matched", Title: "Similar One", Artist: "Other Artist"},
		{ID: "other", Title: "Similar Two", Artist: "Other Artist"},
	}
	for i := 0; i < 10; i++ {
		files = append(files, model.MediaFile{ID: string(rune('k' + i)), Title: "Filler", Annotations: model.Annotations{PlayCount: 2}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{bySeed: map[string][]agents.Song{"seed": {
			{ID: "matched", Name: "Similar One", Artists: []agents.Artist{{Name: "Other Artist"}}},
			{ID: "other", Name: "Similar Two", Artists: []agents.Artist{{Name: "Other Artist"}}},
		}}},
		matcher: matcher.New(ds),
	}
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	mainIDs := map[string]bool{}
	for _, item := range response.Items {
		if item.Kind != model.QuickPickRecommendationKind && item.Song != nil {
			mainIDs[item.Song.ID] = true
		}
		if item.Kind == model.QuickPickRecommendationKind && mainIDs[item.Song.ID] {
			t.Fatalf("Smart Pick %q duplicated a main song", item.Song.ID)
		}
	}
	if len(mainIDs) == 0 {
		t.Fatalf("expected main song tiles, got %#v", response.Items)
	}
}

func TestQuickPickAppliesTasteAffinityToSmartPicks(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Annotations: model.Annotations{PlayCount: 100, Starred: true}},
		{ID: "matched", Title: "Taste Match", Artist: "Other Artist"},
		{ID: "other", Title: "Other Match", Artist: "Other Artist"},
	}
	for i := 0; i < 10; i++ {
		files = append(files, model.MediaFile{ID: string(rune('k' + i)), Title: "Filler", Annotations: model.Annotations{PlayCount: 10, Starred: true}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	metrics := fakeTasteMetrics{
		fakeMetrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		affinity: map[string]recommendations.TasteAffinity{
			"quickpick:matched": recommendations.ComposeTasteAffinity(1, 1, 1, 1),
		},
	}
	svc := &service{
		ds:      ds,
		metrics: metrics,
		agents: fakeSimilarityProvider{bySeed: map[string][]agents.Song{"seed": {
			{ID: "matched", Name: "Taste Match", Artists: []agents.Artist{{Name: "Other Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", NormalizedScore: .4}}},
			{ID: "other", Name: "Other Match", Artists: []agents.Artist{{Name: "Other Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", NormalizedScore: .4}}},
		}}},
		matcher: matcher.New(ds),
	}
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	var matched *model.QuickPickItem
	for _, item := range response.Items {
		if item.Song != nil && item.Song.ID == "matched" {
			copy := item
			matched = &copy
			break
		}
	}
	if matched == nil {
		t.Fatalf("taste affinity did not surface matched local song: %#v", response.Items)
	}
	if matched.Section != model.QuickPickSectionStartRadio {
		t.Fatalf("matched local song should be a radio seed: %#v", matched)
	}
}

func TestQuickPickSeedsSmartPicksFromComposedSongs(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{{ID: "anchor", Title: "Anchor", Annotations: model.Annotations{PlayCount: 100}}}
	for i := 0; i < 18; i++ {
		files = append(files, model.MediaFile{ID: string(rune('a' + i)), Title: "Popular", Annotations: model.Annotations{PlayCount: 10}})
	}
	files = append(files,
		model.MediaFile{ID: "deep", Title: "Deep Song", Annotations: model.Annotations{PlayCount: 1}},
		model.MediaFile{ID: "liked", Title: "Liked Song", Annotations: model.Annotations{PlayCount: 1, Starred: true}},
	)
	media.SetData(files)
	seedIDs := []string{}
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"anchor": 20}},
		agents:  fakeSimilarityProvider{seedIDs: &seedIDs},
		matcher: matcher.New(ds),
	}
	if _, err := svc.Get(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}
	if len(seedIDs) != 0 {
		t.Fatalf("provider seeds = %v, want no GET provider calls", seedIDs)
	}
}

func TestQuickPickRotatesNonAnchorSongsAfterExposure(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{{ID: "anchor", Title: "Anchor", Annotations: model.Annotations{PlayCount: 100}}}
	for i := 0; i < 24; i++ {
		files = append(files, model.MediaFile{ID: string(rune('a' + i)), Title: "Candidate", Annotations: model.Annotations{PlayCount: 10}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	metrics := &trackingMetrics{fakeMetrics: fakeMetrics{recent: map[string]int64{"anchor": 20}}}
	svc := New(ds, metrics, nil, nil)
	first, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	visibleKeys := make([]string, 0, len(first.Items))
	for _, item := range first.Items {
		visibleKeys = append(visibleKeys, item.ItemKey)
	}
	if err := svc.RecordImpressions(context.Background(), "user", first.ViewID, visibleKeys); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	firstIDs := selectedSongIDs(first.Items)
	secondIDs := selectedSongIDs(second.Items)
	if len(firstIDs) != len(secondIDs) || len(firstIDs) < 2 {
		t.Fatalf("first=%v second=%v; want comparable multi-song pages", firstIDs, secondIDs)
	}
	if firstIDs[0] != "anchor" || secondIDs[0] != "anchor" {
		t.Fatalf("anchor changed: first=%v second=%v", firstIDs, secondIDs)
	}
	changed := false
	for index := 1; index < len(firstIDs); index++ {
		if firstIDs[index] != secondIDs[index] {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatalf("non-anchor songs did not rotate: first=%v second=%v", firstIDs, secondIDs)
	}
}

func TestQuickPickDoesNotCallSimilarityProvidersOrRecordGETExposures(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{}
	for i := 0; i < 20; i++ {
		files = append(files, model.MediaFile{ID: string(rune('a' + i)), Title: "Song", Artist: string(rune('a' + i)), Annotations: model.Annotations{PlayCount: int64(100 - i)}})
	}
	media.SetData(files)
	seedIDs := []string{}
	recorded := map[string]int{}
	metrics := fakeMetrics{recent: map[string]int64{}, recorded: recorded}
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{ds: ds, metrics: metrics, agents: fakeSimilarityProvider{seedIDs: &seedIDs}, matcher: matcher.New(ds)}
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 12 {
		t.Fatalf("got %d items, want 12", len(response.Items))
	}
	if len(seedIDs) != 0 {
		t.Fatalf("similarity provider seeds = %v, want no provider calls", seedIDs)
	}
	if len(recorded) != 0 {
		t.Fatalf("GET recorded exposures = %#v, want none", recorded)
	}
	if response.ViewID == "" {
		t.Fatal("response viewId is empty")
	}
	for _, item := range response.Items {
		if item.ViewID != response.ViewID || item.ItemKey == "" || item.Section == "" {
			t.Fatalf("item telemetry metadata = %#v, want viewId/itemKey/section", item)
		}
	}
}
