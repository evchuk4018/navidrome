package personalradio

import (
	"context"
	"testing"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	musicservice "github.com/navidrome/navidrome/core/music"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

type fakeSimilarityProvider struct {
	songs []agents.Song
}

func (f fakeSimilarityProvider) GetSimilarSongsByTrackAll(context.Context, string, string, string, string, int) ([]agents.Song, error) {
	return f.songs, nil
}

type fakePersonalRadioRepository struct {
	model.PersonalRadioRepository
	items   []model.PersonalRadioItem
	session *model.PersonalRadioSession
	feedback map[string]model.RadioTrackFeedback
}

func (f *fakePersonalRadioRepository) GetSessionForUser(string, string) (*model.PersonalRadioSession, error) {
	if f.session == nil {
		return &model.PersonalRadioSession{ID: "session", Status: model.PersonalRadioEnded}, nil
	}
	session := *f.session
	return &session, nil
}

func (f *fakePersonalRadioRepository) GetItems(string) ([]model.PersonalRadioItem, error) {
	return append([]model.PersonalRadioItem(nil), f.items...), nil
}

func (f *fakePersonalRadioRepository) AppendItems(_ string, items []model.PersonalRadioItem) error {
	f.items = append(f.items, items...)
	return nil
}

func (f *fakePersonalRadioRepository) GetFeedback(string, []string) (map[string]model.RadioTrackFeedback, error) {
	return f.feedback, nil
}

func (f *fakePersonalRadioRepository) UpdateItem(item *model.PersonalRadioItem) error {
	for i := range f.items {
		if f.items[i].ID == item.ID {
			f.items[i] = *item
			return nil
		}
	}
	f.items = append(f.items, *item)
	return nil
}

func (f *fakePersonalRadioRepository) UpsertDiscovery(*model.DiscoveryTrack) error {
	return nil
}

type fakeMusicService struct {
	musicservice.Service
	requests []model.ExternalDownloadRequest
	job      *model.MusicDownloadJob
}

func (f *fakeMusicService) CreateDownload(_ context.Context, _ string, request model.ExternalDownloadRequest) (*model.MusicDownloadJob, error) {
	f.requests = append(f.requests, request)
	return &model.MusicDownloadJob{ID: "job-" + request.ID}, nil
}

func (f *fakeMusicService) GetDownload(context.Context, string, string) (*model.MusicDownloadJob, error) {
	return f.job, nil
}

func TestEarlySkipThreshold(t *testing.T) {
	for _, test := range []struct {
		duration int64
		want     int64
	}{{210000, 30000}, {80000, 16000}, {0, 30000}} {
		if got := earlySkipThresholdMS(test.duration); got != test.want {
			t.Fatalf("earlySkipThresholdMS(%d) = %d, want %d", test.duration, got, test.want)
		}
	}
}

func TestLocalCandidatesStayInSeedGenre(t *testing.T) {
	repo := tests.CreateMockMediaFileRepo()
	repo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "21 Savage", Genre: "Rap"},
		{ID: "rap", Artist: "Future", Genre: "Rap", Annotations: model.Annotations{PlayCount: 10}},
		{ID: "rock", Artist: "Queen", Genre: "Rock", Annotations: model.Annotations{PlayCount: 1000}},
	})
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}
	result, err := svc.localCandidates(context.Background(), repo.Data["seed"], map[string]bool{"seed": true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "rap" {
		t.Fatalf("expected genre-compatible track, got %#v", result)
	}
}

func TestGenreSimilarityAllowsGenreFamilies(t *testing.T) {
	tests := []struct {
		seed      string
		candidate string
		want      bool
	}{
		{seed: "Pop", candidate: "Indie Pop", want: true},
		{seed: "Hip-Hop", candidate: "Rap", want: true},
		{seed: "Rock", candidate: "Metal", want: false},
	}
	for _, test := range tests {
		got := genreSimilarity(test.seed, test.candidate) > 0
		if got != test.want {
			t.Fatalf("genreSimilarity(%q, %q) = %v, want %v", test.seed, test.candidate, got, test.want)
		}
	}
}

func TestLocalCandidatesDoNotLetPopularityOverrideGenre(t *testing.T) {
	repo := tests.CreateMockMediaFileRepo()
	repo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "indie", Artist: "New Artist", Genre: "Indie Pop", Annotations: model.Annotations{PlayCount: 2}},
		{ID: "metal", Artist: "Popular Artist", Genre: "Metal", Annotations: model.Annotations{PlayCount: 100000}},
	})
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}
	result, err := svc.localCandidates(context.Background(), repo.Data["seed"], map[string]bool{"seed": true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "indie" {
		t.Fatalf("expected indie-pop candidate, got %#v", result)
	}
}

func TestLocalCandidatesDoNotFallBackToUnrelatedLibrary(t *testing.T) {
	repo := tests.CreateMockMediaFileRepo()
	repo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "unrelated", Artist: "Other Artist", Genre: "Metal", Annotations: model.Annotations{PlayCount: 100000}},
	})
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}
	result, err := svc.localCandidates(context.Background(), repo.Data["seed"], map[string]bool{"seed": true}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected no unrelated fallback tracks, got %#v", result)
	}
}

func TestRecommendationPoolsRankProviderCandidatesAndPreserveFiltering(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "local-low", Title: "Local Low", Artist: "Local Artist", Genre: "Pop"},
		{ID: "local-high", Title: "Local High", Artist: "Local Artist", Genre: "Pop"},
		{ID: "local-seen", Title: "Local Seen", Artist: "Local Artist", Genre: "Pop"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	svc := &service{
		ds:      ds,
		repo:    &fakePersonalRadioRepository{},
		agents: fakeSimilarityProvider{songs: []agents.Song{
			{ID: "local-low", Name: "Local Low", Artists: []agents.Artist{{Name: "Local Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.1, NormalizedScore: 0.1}}},
			{ID: "external-low", Name: "External Low", MBID: "external-low-mbid", Artists: []agents.Artist{{Name: "External Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.2, NormalizedScore: 0.2}}},
			{ID: "local-high", Name: "Local High", Artists: []agents.Artist{{Name: "Local Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.9, NormalizedScore: 0.9}}},
			{ID: "external-high", Name: "External High", MBID: "external-high-mbid", Artists: []agents.Artist{{Name: "External Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.8, NormalizedScore: 0.8}}},
			{ID: "local-seen", Name: "Local Seen", Artists: []agents.Artist{{Name: "Local Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 1, NormalizedScore: 1}}},
			{ID: "external-seen", Name: "External Seen", MBID: "external-seen-mbid", Artists: []agents.Artist{{Name: "External Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.7, NormalizedScore: 0.7}}},
			{ID: "external-no-mbid", Name: "External Without MBID", Artists: []agents.Artist{{Name: "External Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.6, NormalizedScore: 0.6}}},
		}},
		matcher: matcher.New(ds),
	}

	pools, err := svc.recommendationPools(
		context.Background(),
		model.PersonalRadioSession{ID: "session", UserID: "user"},
		mediaRepo.Data["seed"],
		map[string]bool{"seed": true, "local-seen": true},
		map[string]bool{"external-seen-mbid": true},
		4,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.local) != 2 || pools.local[0].ID != "local-high" || pools.local[1].ID != "local-low" {
		t.Fatalf("local pool = %#v, want [local-high local-low]", pools.local)
	}
	if len(pools.discovery) != 2 || pools.discovery[0].MBID != "external-high-mbid" || pools.discovery[1].MBID != "external-low-mbid" {
		t.Fatalf("discovery pool = %#v, want [external-high-mbid external-low-mbid]", pools.discovery)
	}
}

func TestRecommendationPoolsPenalizeFatiguedDiscoveries(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{{ID: "seed", Title: "Seed", Artist: "Seed Artist", Genre: "Pop"}})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	old := time.Now().UTC().Add(-400 * 24 * time.Hour)
	repo := &fakePersonalRadioRepository{feedback: map[string]model.RadioTrackFeedback{
		"fatigued-mbid": {EarlySkipCount: 5, LastEarlySkipAt: &old},
	}}
	svc := &service{
		ds:   ds,
		repo: repo,
		agents: fakeSimilarityProvider{songs: []agents.Song{
			{Name: "Fatigued", MBID: "fatigued-mbid", Artists: []agents.Artist{{Name: "External Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", NormalizedScore: 0.9}}},
			{Name: "Fresh", MBID: "fresh-mbid", Artists: []agents.Artist{{Name: "External Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", NormalizedScore: 0.2}}},
		}},
		matcher: matcher.New(ds),
	}

	pools, err := svc.recommendationPools(
		context.Background(),
		model.PersonalRadioSession{ID: "session", UserID: "user"},
		mediaRepo.Data["seed"],
		map[string]bool{"seed": true},
		nil,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.discovery) != 2 || pools.discovery[0].MBID != "fresh-mbid" || pools.discovery[1].MBID != "fatigued-mbid" {
		t.Fatalf("discovery pool = %#v, want [fresh-mbid fatigued-mbid]", pools.discovery)
	}
}

func TestLocalCandidatesPreferArtistAffinityOverPopularity(t *testing.T) {
	repo := tests.CreateMockMediaFileRepo()
	repo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "same-artist", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "same-genre", Artist: "Other Artist", Genre: "Pop", Annotations: model.Annotations{PlayCount: 100000}},
	})
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}
	result, err := svc.localCandidates(context.Background(), repo.Data["seed"], map[string]bool{"seed": true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "same-artist" {
		t.Fatalf("expected same-artist candidate, got %#v", result)
	}
}

func TestPlanQueuesOrderedDiscoveriesWithReadyLibraryBuffers(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed", Artist: "Seed Artist", Genre: "Pop", MbzRecordingID: "seed-mbid"},
		{ID: "local-1", Title: "Local One", Artist: "Similar Artist", Genre: "Pop"},
		{ID: "local-2", Title: "Local Two", Artist: "Similar Artist", Genre: "Pop"},
		{ID: "local-3", Title: "Local Three", Artist: "Similar Artist", Genre: "Pop"},
		{ID: "local-4", Title: "Local Four", Artist: "Similar Artist", Genre: "Pop"},
		{ID: "local-5", Title: "Local Five", Artist: "Similar Artist", Genre: "Pop"},
		{ID: "local-6", Title: "Local Six", Artist: "Similar Artist", Genre: "Pop"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{items: []model.PersonalRadioItem{{
		ID: "seed-item", SessionID: "session", Position: 0, ItemType: model.RadioItemSeed,
		Status: model.RadioItemReady, MediaFileID: "seed", RecordingMBID: "seed-mbid",
	}}}
	music := &fakeMusicService{}
	svc := &service{
		ds:   ds,
		repo: repo,
		agents: fakeSimilarityProvider{songs: []agents.Song{
			{ID: "local-1"}, {MBID: "fresh-1", Name: "Fresh One", Artists: []agents.Artist{{Name: "Fresh Artist"}}, Album: "Fresh Album"},
			{ID: "local-2"}, {MBID: "fresh-2", Name: "Fresh Two", Artists: []agents.Artist{{Name: "Fresh Artist"}}, Album: "Fresh Album"},
			{ID: "local-3"}, {MBID: "fresh-3"},
			{ID: "local-4"}, {MBID: "fresh-4"},
			{ID: "local-5"}, {MBID: "fresh-5"},
			{ID: "local-6"}, {MBID: "fresh-6"},
		}},
		matcher:        matcher.New(ds),
		music:          music,
		planning:       map[string]bool{},
		planningStatus: map[string]string{},
	}
	session := model.PersonalRadioSession{ID: "session", UserID: "user"}
	if err := svc.plan(context.Background(), session, mediaRepo.Data["seed"]); err != nil {
		t.Fatal(err)
	}

	if len(repo.items) != 11 {
		t.Fatalf("expected seed plus ten planned items, got %d", len(repo.items))
	}
	// Discovery items lead so the fresh similar tracks are visible in the queue
	// immediately; each is followed by a ready library buffer so playback never
	// stalls while the download lands.
	wantTypes := []string{
		model.RadioItemDiscovery, model.RadioItemLibrary,
		model.RadioItemDiscovery, model.RadioItemLibrary,
		model.RadioItemDiscovery, model.RadioItemLibrary,
		model.RadioItemDiscovery, model.RadioItemLibrary,
		model.RadioItemDiscovery, model.RadioItemLibrary,
	}
	wantStatuses := []string{
		model.RadioItemDownloading, model.RadioItemReady,
		model.RadioItemDownloading, model.RadioItemReady,
		model.RadioItemDownloading, model.RadioItemReady,
		model.RadioItemDownloading, model.RadioItemReady,
		model.RadioItemDownloading, model.RadioItemReady,
	}
	for i, item := range repo.items[1:] {
		if item.Position != i+1 || item.ItemType != wantTypes[i] || item.Status != wantStatuses[i] {
			t.Fatalf("item %d = position %d, type %q, status %q", i, item.Position, item.ItemType, item.Status)
		}
	}
	if len(music.requests) != 5 {
		t.Fatalf("expected five discovery downloads, got %d", len(music.requests))
	}
	for i, request := range music.requests {
		if request.ID != []string{"fresh-1", "fresh-2", "fresh-3", "fresh-4", "fresh-5"}[i] || request.Origin != model.MusicDownloadOriginRadio {
			t.Fatalf("unexpected discovery request %#v", request)
		}
	}
	// The Last.fm recommendation metadata rides on the download request so the
	// queue can show what is being fetched while the download is in flight.
	for i, request := range music.requests[:2] {
		want := []struct {
			title, artist, album string
		}{{"Fresh One", "Fresh Artist", "Fresh Album"}, {"Fresh Two", "Fresh Artist", "Fresh Album"}}[i]
		if request.Title != want.title || request.Artist != want.artist || request.Album != want.album {
			t.Fatalf("recommendation metadata missing from request %#v", request)
		}
	}

	// Polling while all ten slots are ready or downloading must not enqueue
	// another plan or flood the queue with library tracks.
	if err := svc.plan(context.Background(), session, mediaRepo.Data["seed"]); err != nil {
		t.Fatal(err)
	}
	if len(repo.items) != 11 || len(music.requests) != 5 {
		t.Fatalf("in-flight work was duplicated: %d items, %d downloads", len(repo.items), len(music.requests))
	}

	// A failed discovery no longer counts toward capacity. The next plan skips
	// its seen MBID and appends the next ranked discovery/buffer pair.
	repo.items[1].Status = model.RadioItemFailed
	if err := svc.plan(context.Background(), session, mediaRepo.Data["seed"]); err != nil {
		t.Fatal(err)
	}
	if len(repo.items) != 13 || len(music.requests) != 6 {
		t.Fatalf("expected one replacement pair: %d items, %d downloads", len(repo.items), len(music.requests))
	}
	if replacement := music.requests[5]; replacement.ID != "fresh-6" {
		t.Fatalf("expected failed discovery to advance to fresh-6, got %#v", replacement)
	}
	if repo.items[11].ItemType != model.RadioItemDiscovery || repo.items[11].Status != model.RadioItemDownloading {
		t.Fatalf("expected replacement discovery first, got %#v", repo.items[11])
	}
	if repo.items[12].ItemType != model.RadioItemLibrary || repo.items[12].Status != model.RadioItemReady {
		t.Fatalf("expected replacement library buffer, got %#v", repo.items[12])
	}
}

func TestOutstandingCountsReadyAndDownloadingBuffers(t *testing.T) {
	now := time.Now().UTC()
	items := []model.PersonalRadioItem{
		{ItemType: model.RadioItemSeed, Status: model.RadioItemReady, CreatedAt: now},
		{ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, CreatedAt: now},
		{ItemType: model.RadioItemDiscovery, Status: model.RadioItemFailed, CreatedAt: now},
		{ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, CreatedAt: now},
		{ItemType: model.RadioItemDiscovery, Status: model.RadioItemDownloading, CreatedAt: now},
		{ItemType: model.RadioItemLibrary, Status: model.RadioItemPlayed, CreatedAt: now},
	}
	if got := outstandingRadioItems(items); got != 3 {
		t.Fatalf("expected seed excluded, failed excluded, played excluded, got %d", got)
	}
}

func TestRefillFailsCompletedDownloadWithoutLibraryMatch(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{
		session: &model.PersonalRadioSession{ID: "session", UserID: "user", Status: model.PersonalRadioEnded},
		items: []model.PersonalRadioItem{{
			ID: "item", SessionID: "session", Position: 1,
			ItemType: model.RadioItemDiscovery, Status: model.RadioItemDownloading,
			RecordingMBID: "missing-recording", DownloadJobID: "job",
		}},
	}
	music := &fakeMusicService{job: &model.MusicDownloadJob{
		ID: "job", Status: model.MusicDownloadSuccess, SourceID: "missing-recording",
	}}
	svc := &service{
		ds:             ds,
		repo:           repo,
		matcher:        matcher.New(ds),
		music:          music,
		planning:       map[string]bool{},
		planningStatus: map[string]string{},
	}

	response, err := svc.Refill(context.Background(), "user", "session")
	if err != nil {
		t.Fatalf("Refill returned error: %v", err)
	}
	if repo.items[0].Status != model.RadioItemFailed {
		t.Fatalf("expected unmatched completed download to fail, got %q", repo.items[0].Status)
	}
	if response.PlanningStatus != model.RadioPlanningNoDiscovery {
		t.Fatalf("expected terminal no-discovery status after failed completed download, got %q", response.PlanningStatus)
	}
}

func TestRefillExposesPendingDownloadMetadata(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{
		session: &model.PersonalRadioSession{ID: "session", UserID: "user", Status: model.PersonalRadioEnded},
		items: []model.PersonalRadioItem{{
			ID: "item", SessionID: "session", Position: 1,
			ItemType: model.RadioItemDiscovery, Status: model.RadioItemDownloading,
			RecordingMBID: "fresh-mbid", DownloadJobID: "job",
		}},
	}
	music := &fakeMusicService{job: &model.MusicDownloadJob{
		ID: "job", Status: model.MusicDownloadRunning,
		Title: "Fresh Track", Artist: "Fresh Artist", Album: "Fresh Album",
	}}
	svc := &service{
		ds:             ds,
		repo:           repo,
		music:          music,
		planning:       map[string]bool{},
		planningStatus: map[string]string{},
	}

	response, err := svc.Refill(context.Background(), "user", "session")
	if err != nil {
		t.Fatalf("Refill returned error: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].Song == nil {
		t.Fatalf("expected pending item to carry recommendation metadata, got %#v", response.Items)
	}
	song := response.Items[0].Song
	if song.Title != "Fresh Track" || song.Artist != "Fresh Artist" || song.Album != "Fresh Album" {
		t.Fatalf("expected recommendation metadata on pending song, got %#v", song)
	}
	if song.ID != "" {
		t.Fatalf("expected stub song to have no media file id, got %q", song.ID)
	}
}
