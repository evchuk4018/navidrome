package personalradio

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

type contextMatchingStore struct {
	*tests.MockDataStore
	queries []context.Context
}

func (s *contextMatchingStore) MediaFile(ctx context.Context) model.MediaFileRepository {
	return &contextMatchingRepository{MediaFileRepository: s.MockDataStore.MediaFile(ctx), ctx: ctx, store: s}
}

type contextMatchingRepository struct {
	model.MediaFileRepository
	ctx   context.Context
	store *contextMatchingStore
}

func (r *contextMatchingRepository) GetAll(options ...model.QueryOptions) (model.MediaFiles, error) {
	r.store.queries = append(r.store.queries, r.ctx)
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	return r.MediaFileRepository.GetAll(options...)
}

func matchingTestService() (*service, *contextMatchingStore, *model.MediaFile) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed", Artist: "Artist", Genre: "Pop"},
		{ID: "target", Title: "Target", Artist: "Artist", Genre: "Pop", MbzRecordingID: "target-mbid"},
	})
	ds := &contextMatchingStore{MockDataStore: &tests.MockDataStore{MockedMediaFile: mediaRepo}}
	svc := &service{ds: ds, repo: &fakePersonalRadioRepository{}, matcher: matcher.New(ds)}
	return svc, ds, mediaRepo.Data["seed"]
}

func TestRadioMatchingSurvivesExpiredProviderBudget(t *testing.T) {
	svc, ds, seed := matchingTestService()
	svc.agents = fakeSimilarityProvider{songs: []agents.Song{{ID: "target", Name: "Target", MBID: "target-mbid", Artists: []agents.Artist{{Name: "Artist"}}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 0.9}}}}}
	providerCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	pools, err := svc.recommendationPoolsWithLimitContext(context.Background(), providerCtx,
		model.PersonalRadioSession{ID: "session", UserID: "user", Mode: model.RadioModeRelated}, seed,
		map[string]bool{"seed": true}, map[string]bool{}, 1, 40, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range pools.ranked {
		if candidate.local != nil && candidate.local.ID == "target" && candidate.source == relatedProviderStrong {
			found = true
		}
	}
	if !found {
		t.Fatalf("provider candidates were lost: %#v", pools.ranked)
	}
	if len(ds.queries) == 0 {
		t.Fatal("library was not queried")
	}
	matchingCtx := ds.queries[0]
	if deadline, ok := matchingCtx.Deadline(); !ok || time.Until(deadline) < 4*time.Second {
		t.Fatal("matching did not receive a fresh five-second budget")
	}
	if !errors.Is(matchingCtx.Err(), context.Canceled) {
		t.Fatal("matching context was not released")
	}
}

func TestRadioMatchingRespectsParentCancellationAndOwnDeadline(t *testing.T) {
	for _, test := range []struct {
		name     string
		timeout  time.Duration
		cancel   bool
		expected error
	}{
		{"parent cancellation", time.Second, true, context.Canceled},
		{"matching deadline", -time.Nanosecond, false, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _, _ := matchingTestService()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			_, err := svc.matchRadioSongsWithin(ctx, []agents.Song{{ID: "target"}}, test.timeout)
			if !errors.Is(err, test.expected) {
				t.Fatalf("error = %v, want %v", err, test.expected)
			}
		})
	}
}

type failedRadioProvider struct{ fakeSimilarityProvider }

func (failedRadioProvider) GetSimilarSongsByTrack(context.Context, string, string, string, string, int) ([]agents.Song, error) {
	return nil, context.DeadlineExceeded
}

func TestRadioProviderTimeoutStillUsesLocalFallback(t *testing.T) {
	svc, _, seed := matchingTestService()
	svc.agents = failedRadioProvider{}
	pools, err := svc.recommendationPoolsWithLimit(context.Background(), model.PersonalRadioSession{ID: "session", UserID: "user", Mode: model.RadioModeRelated}, seed, map[string]bool{"seed": true}, map[string]bool{}, 1, 40, 1)
	if err != nil || len(pools.local) == 0 {
		t.Fatalf("local fallback = %#v, %v", pools, err)
	}
}
