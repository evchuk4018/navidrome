package personalradio

import (
	"context"
	"testing"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	"github.com/navidrome/navidrome/utils/cache"
)

type relatedSimilarityProvider struct {
	singleCalls []string
	allCalls    int
	songs       []agents.Song
}

type relatedInstantMixProvider struct {
	files model.MediaFiles
	calls int
}

func (p *relatedInstantMixProvider) SimilarSongs(context.Context, string, int) (model.MediaFiles, error) {
	p.calls++
	return p.files, nil
}

type relatedArtistProvider struct {
	byArtist map[string][]agents.Song
	similar  []agents.Artist
}

func (p *relatedArtistProvider) GetSimilarArtists(context.Context, string, string, string, int) ([]agents.Artist, error) {
	return p.similar, nil
}

func (p *relatedArtistProvider) GetArtistTopSongs(_ context.Context, _ string, name, _ string, _ int) ([]agents.Song, error) {
	return p.byArtist[name], nil
}

func TestRelatedRadioUsesInstantMixSongsAndIgnoresGenericGenres(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Heart Attack", Artist: "Demi Lovato", Genres: model.Genres{{Name: "Music"}}},
		{ID: "demi", Title: "Cool for the Summer", Artist: "Demi Lovato", Genres: model.Genres{{Name: "Music"}}},
		{ID: "katy", Title: "Swish Swish", Artist: "Katy Perry", Genres: model.Genres{{Name: "Music"}}},
		{ID: "radiohead", Title: "Creep", Artist: "Radiohead", Genres: model.Genres{{Name: "Music"}}},
		{ID: "drake", Title: "0 to 100", Artist: "Drake", Genres: model.Genres{{Name: "Music"}}},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	instantMix := &relatedInstantMixProvider{files: model.MediaFiles{*mediaRepo.Data["demi"], *mediaRepo.Data["katy"]}}
	svc := &service{ds: ds, repo: &fakePersonalRadioRepository{}, agents: &relatedSimilarityProvider{},
		matcher: matcher.New(ds), relatedSongs: instantMix}
	seed := mediaRepo.Data["seed"]
	pools, err := svc.recommendationPools(context.Background(), model.PersonalRadioSession{
		ID: "session", UserID: "user", Mode: model.RadioModeRelated,
	}, seed, map[string]bool{"seed": true}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if instantMix.calls != 1 || len(pools.local) != 2 || len(pools.discovery) != 0 {
		t.Fatalf("Instant Mix calls=%d, local=%v, discovery=%v", instantMix.calls, pools.local, pools.discovery)
	}
	for _, candidate := range pools.ranked {
		if candidate.local == nil || candidate.source != relatedArtist ||
			(candidate.local.ID != "demi" && candidate.local.ID != "katy") {
			t.Fatalf("unrelated candidate entered related radio: %+v", candidate)
		}
	}
	if relatedLocalTier(seed, *mediaRepo.Data["radiohead"]) != 0 ||
		relatedLocalTier(seed, *mediaRepo.Data["drake"]) != 0 {
		t.Fatal("generic Music genre created a false relationship")
	}
}

func TestRelatedRadioIgnoresVideoCategoriesButKeepsRealGenres(t *testing.T) {
	for _, category := range []string{"Music", "People & Blogs", "Entertainment", "Gaming", "Travel & Events"} {
		seed := &model.MediaFile{ID: "seed", Artist: "Demi Lovato", Genres: model.Genres{{Name: category}}}
		candidate := model.MediaFile{ID: "candidate", Artist: "Radiohead", Genres: model.Genres{{Name: category}}}
		if tier := relatedLocalTier(seed, candidate); tier != 0 {
			t.Errorf("category %q produced related tier %d", category, tier)
		}
	}
	seed := &model.MediaFile{ID: "seed", Artist: "Demi Lovato", Genre: "Pop"}
	if tier := relatedLocalTier(seed, model.MediaFile{ID: "candidate", Artist: "Katy Perry", Genre: "Pop"}); tier == 0 {
		t.Fatal("real Pop genre was filtered")
	}
}

func TestRelatedRadioArtistSuggestionsBecomeDiscoveryCandidates(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Heart Attack", Artist: "Demi Lovato", Genre: "Music"},
		{ID: "local", Title: "Cool for the Summer", Artist: "Demi Lovato", Genre: "Music"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	svc := &service{
		ds: ds, repo: &fakePersonalRadioRepository{}, agents: &relatedSimilarityProvider{}, matcher: matcher.New(ds),
		relatedSongs: &relatedInstantMixProvider{files: model.MediaFiles{*mediaRepo.Data["local"]}},
		artistAgents: &relatedArtistProvider{byArtist: map[string][]agents.Song{
			"Demi Lovato": {{Name: "Unowned Demi song", MBID: "new-demi-mbid"}},
			"Katy Perry":  {{Name: "Unowned Katy song", MBID: "new-katy-mbid"}},
		}, similar: []agents.Artist{{Name: "Katy Perry"}}},
	}
	seed := mediaRepo.Data["seed"]
	pools, err := svc.recommendationPools(context.Background(), model.PersonalRadioSession{
		ID: "session", UserID: "user", Mode: model.RadioModeRelated,
	}, seed, map[string]bool{"seed": true}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.local) != 1 || len(pools.discovery) != 2 {
		t.Fatalf("local=%v, discovery=%v; want one local and two download candidates", pools.local, pools.discovery)
	}
	selected := composeRadioCandidates(pools.ranked, radioCompositionOptions{
		Mode: model.RadioModeRelated, Slots: 10, SeedActive: true,
	})
	if len(selected) != 2 || !selected[1].isDiscovery || selected[1].source != relatedArtistDownload {
		t.Fatalf("selected=%+v; want local first and capped artist download", selected)
	}
}

func TestRelatedRadioReplaysOldestRelatedSongsAfterFreshPoolRunsOut(t *testing.T) {
	seed := model.MediaFile{ID: "seed", Artist: "Demi Lovato", Genre: "Music"}
	old := model.MediaFile{ID: "old", Artist: "Katy Perry", Genre: "Music"}
	accepted := model.MediaFile{ID: "accepted", Artist: "Dua Lipa", Genre: "Music"}
	activeFile := model.MediaFile{ID: "active", Artist: "Demi Lovato", Genre: "Music"}
	unrelated := model.MediaFile{ID: "unrelated", Artist: "Drake", Genre: "Music"}
	svc := &service{relatedSongs: &relatedInstantMixProvider{files: model.MediaFiles{old, accepted, activeFile}}}
	items := []model.PersonalRadioItem{
		{MediaFileID: "old", Position: 1, Status: model.RadioItemPlayed, Song: &old, PlaybackOutcome: model.RadioPlaybackEarlySkip},
		{MediaFileID: "accepted", Position: 2, Status: model.RadioItemPlayed, Song: &accepted, PlaybackOutcome: model.RadioPlaybackCompleted},
		{MediaFileID: "active", Position: 3, Status: model.RadioItemReady, Song: &activeFile},
		{MediaFileID: "unrelated", Position: 4, Status: model.RadioItemPlayed, Song: &unrelated},
	}
	candidates := svc.relatedReplayCandidates(context.Background(), items, items[2:3], &seed, nil)
	selected := composeRadioCandidates(candidates, radioCompositionOptions{Mode: model.RadioModeRelated, Slots: 10, Active: items[2:3]})
	if len(selected) != 2 || selected[0].local.ID != "accepted" || selected[1].local.ID != "old" {
		t.Fatalf("replay order=%+v; want accepted then early-skipped old; active/unrelated excluded", selected)
	}
}

func TestRelatedRadioRepeatedRefillsReuseRelatedPool(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Demi Lovato", Genre: "Music"},
		{ID: "one", Artist: "Katy Perry", Genre: "Music"},
		{ID: "two", Artist: "Dua Lipa", Genre: "Music"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{items: []model.PersonalRadioItem{{
		ID: "seed-item", SessionID: "session", Position: 0, ItemType: model.RadioItemSeed,
		Status: model.RadioItemReady, MediaFileID: "seed", Song: mediaRepo.Data["seed"],
	}}}
	svc := &service{ds: ds, repo: repo, agents: &relatedSimilarityProvider{}, matcher: matcher.New(ds),
		relatedSongs:   &relatedInstantMixProvider{files: model.MediaFiles{*mediaRepo.Data["one"], *mediaRepo.Data["two"]}},
		planningStatus: map[string]string{}}
	session := model.PersonalRadioSession{ID: "session", UserID: "user", Mode: model.RadioModeRelated}
	for round := 0; round < 3; round++ {
		if err := svc.planWithContext(context.Background(), session, radioContextFromSeed(mediaRepo.Data["seed"])); err != nil {
			t.Fatal(err)
		}
		if got := len(repo.items); got != 1+2*(round+1) {
			t.Fatalf("round %d has %d items, want %d", round, got, 1+2*(round+1))
		}
		for i := 1; i < len(repo.items); i++ {
			repo.items[i].Status = model.RadioItemPlayed
			repo.items[i].PlaybackOutcome = model.RadioPlaybackCompleted
		}
	}
}

func TestRelatedRadioResolvedDownloadRefreshesInstantMixSource(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Heart Attack", Artist: "Demi Lovato"},
		{ID: "imported", Title: "New Song", Artist: "Katy Perry", MbzRecordingID: "new-recording"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	provider := &relatedInstantMixProvider{}
	repo := &fakePersonalRadioRepository{
		session: &model.PersonalRadioSession{ID: "session", UserID: "user", SeedMediaFileID: "seed",
			Mode: model.RadioModeRelated, Status: model.PersonalRadioEnded},
		items: []model.PersonalRadioItem{{
			ID: "download", SessionID: "session", ItemType: model.RadioItemDiscovery,
			Status: model.RadioItemDownloading, RecordingMBID: "new-recording", DownloadJobID: "job",
		}},
	}
	svc := &service{ds: ds, repo: repo, matcher: matcher.New(ds), relatedSongs: provider,
		relatedLocal: cache.NewSimpleCache[string, model.MediaFiles](),
		music: &fakeMusicService{job: &model.MusicDownloadJob{
			ID: "job", Status: model.MusicDownloadSuccess, Title: "New Song", Artist: "Katy Perry",
		}}, planningStatus: map[string]string{}, planning: map[string]bool{}}
	svc.relatedLocalSongs(context.Background(), mediaRepo.Data["seed"])
	if provider.calls != 1 {
		t.Fatalf("provider calls before resolution = %d", provider.calls)
	}
	if _, err := svc.Refill(context.Background(), "user", "session", model.RefillPersonalRadioRequest{}); err != nil {
		t.Fatal(err)
	}
	if repo.items[0].Status != model.RadioItemReady || repo.items[0].MediaFileID != "imported" {
		t.Fatalf("resolved item = %+v", repo.items[0])
	}
	svc.relatedLocalSongs(context.Background(), mediaRepo.Data["seed"])
	if provider.calls != 2 {
		t.Fatalf("provider calls after resolution = %d; want cache invalidated", provider.calls)
	}
}

func TestRelatedRadioFailedDownloadCanFallBackToReplay(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Demi Lovato", Genre: "Music"},
		{ID: "related", Artist: "Katy Perry", Genre: "Music"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{
		session: &model.PersonalRadioSession{ID: "session", UserID: "user", SeedMediaFileID: "seed",
			Mode: model.RadioModeRelated, Status: model.PersonalRadioEnded},
		items: []model.PersonalRadioItem{
			{ID: "seed-item", SessionID: "session", ItemType: model.RadioItemSeed,
				MediaFileID: "seed", Status: model.RadioItemReady, Song: mediaRepo.Data["seed"]},
			{ID: "played", SessionID: "session", Position: 1, ItemType: model.RadioItemLibrary,
				MediaFileID: "related", Status: model.RadioItemPlayed, Song: mediaRepo.Data["related"]},
			{ID: "failed", SessionID: "session", Position: 2, ItemType: model.RadioItemDiscovery,
				Status: model.RadioItemDownloading, RecordingMBID: "failed-recording", DownloadJobID: "job"},
		},
	}
	svc := &service{ds: ds, repo: repo, agents: &relatedSimilarityProvider{}, matcher: matcher.New(ds),
		relatedSongs:   &relatedInstantMixProvider{files: model.MediaFiles{*mediaRepo.Data["related"]}},
		music:          &fakeMusicService{job: &model.MusicDownloadJob{ID: "job", Status: model.MusicDownloadFailed}},
		planningStatus: map[string]string{}, planning: map[string]bool{}}
	if _, err := svc.Refill(context.Background(), "user", "session", model.RefillPersonalRadioRequest{}); err != nil {
		t.Fatal(err)
	}
	if repo.items[2].Status != model.RadioItemFailed {
		t.Fatalf("failed download status = %s", repo.items[2].Status)
	}
	if err := svc.planWithContext(context.Background(), *repo.session, radioContextFromSeed(mediaRepo.Data["seed"])); err != nil {
		t.Fatal(err)
	}
	if len(repo.items) != 4 || repo.items[3].MediaFileID != "related" {
		t.Fatalf("queue after download failure = %+v; want related replay", repo.items)
	}
}

func (p *relatedSimilarityProvider) GetSimilarSongsByTrack(_ context.Context, id, _, _, _ string, _ int) ([]agents.Song, error) {
	p.singleCalls = append(p.singleCalls, id)
	return p.songs, nil
}

func (p *relatedSimilarityProvider) GetSimilarSongsByTrackAll(context.Context, string, string, string, string, int) ([]agents.Song, error) {
	p.allCalls++
	return nil, nil
}

func TestRelatedRadioUsesFirstProviderAndOriginalSeedOnRefill(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Pop Seed", Artist: "Pop Artist", Genre: "Pop"},
		{ID: "current", Title: "Current", Artist: "Rock Artist", Genre: "Rock"},
		{ID: "local-pop", Title: "Local Pop", Artist: "Another Pop Artist", Genre: "Pop"},
		{ID: "unrelated", Title: "Metal Track", Artist: "Metal Artist", Genre: "Metal", Year: 2020},
	})
	provider := &relatedSimilarityProvider{songs: []agents.Song{
		{Name: "Strong Pop", MBID: "strong-pop-mbid", Artists: []agents.Artist{{Name: "Pop Artist"}}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 0.9}}},
		{Name: "Broader Pop", MBID: "broader-pop-mbid", Artists: []agents.Artist{{Name: "Another Pop Artist"}}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 0.4}}},
		{Name: "Weak Metal", MBID: "weak-metal-mbid", Artists: []agents.Artist{{Name: "Metal Artist"}}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 0.1}}},
	}}
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	svc := &service{ds: ds, repo: &fakePersonalRadioRepository{}, agents: provider, matcher: matcher.New(ds)}
	seed := mediaRepo.Data["seed"]
	current := mediaRepo.Data["current"]
	pools, err := svc.recommendationPoolsForContext(context.Background(), model.PersonalRadioSession{
		ID: "session", UserID: "user", Mode: model.RadioModeRelated,
	}, &radioContext{
		OriginalSeed: seed,
		Seeds:        []radioSeed{{File: seed, Weight: 0.35}, {File: current, Weight: 0.65}},
	}, map[string]bool{"seed": true, "current": true}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.singleCalls) != 1 || provider.singleCalls[0] != "seed" || provider.allCalls != 0 {
		t.Fatalf("provider calls = single %v, all %d; want one first-provider call for original seed", provider.singleCalls, provider.allCalls)
	}
	if len(pools.discovery) != 2 || pools.discovery[0].MBID != "strong-pop-mbid" || pools.discovery[1].MBID != "broader-pop-mbid" {
		t.Fatalf("discovery pool = %#v, want only strong and broader pop", pools.discovery)
	}
	for _, file := range pools.local {
		if file.ID == "unrelated" {
			t.Fatal("unrelated metal track entered related radio fallback")
		}
	}
}

func TestRelatedRadioFallsBackToRelatedLibraryOnly(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Pop Artist", Genre: "Pop", Year: 2020},
		{ID: "close", Artist: "Another Artist", Genre: "Indie Pop"},
		{ID: "family", Artist: "Third Artist", Genre: "Indie"},
		{ID: "year-only", Artist: "Metal Artist", Genre: "Metal", Year: 2020},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	svc := &service{ds: ds, repo: &fakePersonalRadioRepository{}, agents: &relatedSimilarityProvider{}, matcher: matcher.New(ds)}
	seed := mediaRepo.Data["seed"]
	pools, err := svc.recommendationPools(context.Background(), model.PersonalRadioSession{
		ID: "session", UserID: "user", Mode: model.RadioModeRelated,
	}, seed, map[string]bool{"seed": true}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.local) != 2 || len(pools.discovery) != 0 {
		t.Fatalf("fallback pools = local %#v, discovery %#v; want two related library tracks", pools.local, pools.discovery)
	}
	if pools.local[0].ID != "close" || pools.local[1].ID != "family" {
		t.Fatalf("fallback order = [%s %s], want close then family", pools.local[0].ID, pools.local[1].ID)
	}
}

func TestRelatedProviderConfidenceRejectsWeakAndUnscoredTail(t *testing.T) {
	if tier, _ := relatedProviderTier(agents.Song{SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 0.2}}}, 0); tier != 0 {
		t.Fatalf("weak scored candidate tier = %d, want rejected", tier)
	}
	if tier, _ := relatedProviderTier(agents.Song{SimilarityScores: []agents.SimilarityScore{{Provider: "lastfm"}}}, 0); tier != 0 {
		t.Fatalf("zero-score candidate tier = %d, want rejected", tier)
	}
	for _, tc := range []struct{ index, want int }{{0, 1}, {9, 1}, {10, 2}, {19, 2}, {20, 0}} {
		if tier, _ := relatedProviderTier(agents.Song{}, tc.index); tier != tc.want {
			t.Errorf("unscored candidate at %d has tier %d, want %d", tc.index, tier, tc.want)
		}
	}
}

func TestRelatedRadioDiscoveryIsCappedWithoutForcingDownloads(t *testing.T) {
	var candidates []rankedRadioCandidate
	for i := 0; i < 10; i++ {
		local := newCompositionCandidate("local-"+string(rune('a'+i)), false, 0.8)
		local.source = relatedLocalClose
		candidates = append(candidates, local)
		fresh := newCompositionCandidate("discovery-"+string(rune('a'+i)), true, 2)
		fresh.source = relatedProviderStrong
		candidates = append(candidates, fresh)
	}
	selected := composeRadioCandidates(candidates, radioCompositionOptions{Mode: model.RadioModeRelated, Slots: 10, SeedActive: true})
	if len(selected) != 10 {
		t.Fatalf("selected %d candidates, want 10", len(selected))
	}
	if got := countCompositionDiscoveries(selected); got > 3 {
		t.Fatalf("selected %d discoveries in a ten-track queue, want at most three", got)
	}

	for i := range candidates {
		if candidates[i].isDiscovery {
			candidates[i].source = relatedProviderBroad
		}
	}
	selected = composeRadioCandidates(candidates[:2], radioCompositionOptions{Mode: model.RadioModeRelated, Slots: 2, SeedActive: true})
	if got := countCompositionDiscoveries(selected); got != 1 {
		t.Fatalf("selected %d broad discoveries alongside one local track, want one", got)
	}
	selected = composeRadioCandidates(candidates[:2], radioCompositionOptions{Mode: model.RadioModeRelated, Slots: 2})
	if got := countCompositionDiscoveries(selected); got != 0 {
		t.Fatalf("selected %d discoveries in a two-track refill, want none without the playing seed", got)
	}
}

func TestRelatedRadioExhaustsWhenOnlyDownloadsWouldBreakTheCap(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{{ID: "seed", Title: "Pop Seed", Artist: "Pop Artist", Genre: "Pop"}})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{items: []model.PersonalRadioItem{{
		ID: "seed-item", SessionID: "session", ItemType: model.RadioItemSeed,
		Status: model.RadioItemReady, MediaFileID: "seed",
	}}}
	svc := &service{
		ds: ds, repo: repo, matcher: matcher.New(ds), planningStatus: map[string]string{},
		agents: &relatedSimilarityProvider{songs: []agents.Song{{
			Name: "Strong Pop", MBID: "strong-pop-mbid", Artists: []agents.Artist{{Name: "Pop Artist"}},
			SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 0.9}},
		}}},
	}
	seed := mediaRepo.Data["seed"]
	if err := svc.planWithContext(context.Background(), model.PersonalRadioSession{
		ID: "session", UserID: "user", Mode: model.RadioModeRelated,
	}, radioContextFromSeed(seed)); err != nil {
		t.Fatal(err)
	}
	if status := svc.getPlanningStatus("session"); status != model.RadioPlanningExhausted {
		t.Fatalf("planning status = %q, want exhausted", status)
	}
}
