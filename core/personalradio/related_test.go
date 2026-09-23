package personalradio

import (
	"context"
	"testing"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

type relatedSimilarityProvider struct {
	singleCalls []string
	allCalls    int
	songs       []agents.Song
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
