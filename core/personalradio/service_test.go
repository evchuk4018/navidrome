package personalradio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	musicservice "github.com/navidrome/navidrome/core/music"
	"github.com/navidrome/navidrome/core/recommendations"
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
	items          []model.PersonalRadioItem
	session        *model.PersonalRadioSession
	feedback       map[string]model.RadioTrackFeedback
	transitions    map[string]model.RadioTransitionFeedback
	discoveries    map[string]model.DiscoveryTrack
	feedbackEvents []string
}

func (f *fakePersonalRadioRepository) GetSessionForUser(string, string) (*model.PersonalRadioSession, error) {
	if f.session == nil {
		return &model.PersonalRadioSession{ID: "session", Status: model.PersonalRadioEnded}, nil
	}
	session := *f.session
	return &session, nil
}

func (f *fakePersonalRadioRepository) UpdateSession(session *model.PersonalRadioSession) error {
	if f.session == nil {
		f.session = &model.PersonalRadioSession{}
	}
	copy := *session
	f.session = &copy
	return nil
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

func (f *fakePersonalRadioRepository) GetRecentAcceptedItems(sessionID string, limit int) ([]model.PersonalRadioItem, error) {
	var accepted []model.PersonalRadioItem
	for i := len(f.items) - 1; i >= 0 && len(accepted) < limit; i-- {
		item := f.items[i]
		if item.SessionID == sessionID && model.IsAcceptedRadioPlaybackOutcome(item.PlaybackOutcome) {
			accepted = append(accepted, item)
		}
	}
	return accepted, nil
}

func (f *fakePersonalRadioRepository) RecordPlaybackFeedback(userID, sessionID string, request model.PersonalRadioFeedbackRequest, now time.Time) (*model.RadioPlaybackFeedbackResult, error) {
	if f.transitions == nil {
		f.transitions = map[string]model.RadioTransitionFeedback{}
	}
	for i := range f.items {
		item := &f.items[i]
		if item.ID != request.ItemID || item.SessionID != sessionID {
			continue
		}
		if userID == "" {
			return nil, model.ErrNotFound
		}
		item.Status = model.RadioItemPlayed
		if request.ListenedMS > item.ListenedMS {
			item.ListenedMS = request.ListenedMS
		}
		if request.DurationMS > item.DurationMS {
			item.DurationMS = request.DurationMS
		}
		item.LastFeedbackAt = &now
		applied := false
		var delta string
		switch request.Event {
		case model.RadioFeedbackStarted:
			if item.PlaybackOutcome == "" {
				item.PlaybackOutcome, applied = model.RadioPlaybackStarted, true
				if source := f.latestAccepted(sessionID, item.ID); source != nil && item.TransitionSourceKey == "" {
					item.TransitionSourceItemID = source.ID
					item.TransitionSourceKey = model.RadioTrackKey(source.RecordingMBID, source.MediaFileID)
				}
				delta = "attempt"
			}
		case model.RadioFeedbackThresholdReached:
			if item.PlaybackOutcome != model.RadioPlaybackCompleted && item.PlaybackOutcome != model.RadioPlaybackLateSkip && item.PlaybackOutcome != model.RadioPlaybackKeep && item.PlaybackOutcome != model.RadioPlaybackAccepted {
				item.PlaybackOutcome, applied, delta = model.RadioPlaybackAccepted, true, "accepted"
			}
		case model.RadioFeedbackCompleted:
			if item.PlaybackOutcome != model.RadioPlaybackCompleted {
				item.PlaybackOutcome, applied, delta = model.RadioPlaybackCompleted, true, "completed"
			}
		case model.RadioFeedbackManualSkip:
			if item.PlaybackOutcome != model.RadioPlaybackCompleted && item.PlaybackOutcome != model.RadioPlaybackKeep && item.PlaybackOutcome != model.RadioPlaybackLateSkip {
				threshold := int64(30000)
				if item.DurationMS > 0 {
					threshold = minInt64ForTest(threshold, item.DurationMS/5)
				}
				if model.IsAcceptedRadioPlaybackOutcome(item.PlaybackOutcome) || item.ListenedMS >= threshold {
					item.PlaybackOutcome, applied, delta = model.RadioPlaybackLateSkip, true, "neutral"
				} else if item.PlaybackOutcome != model.RadioPlaybackEarlySkip {
					item.PlaybackOutcome, applied, delta = model.RadioPlaybackEarlySkip, true, "early"
				}
			}
		case model.RadioFeedbackKeep:
			if item.PlaybackOutcome != model.RadioPlaybackCompleted && item.PlaybackOutcome != model.RadioPlaybackKeep {
				item.PlaybackOutcome, applied, delta = model.RadioPlaybackKeep, true, "keep"
			}
		}
		if applied && item.TransitionSourceKey != "" && delta != "" {
			key := item.TransitionSourceKey + "\x00" + model.RadioTrackKey(item.RecordingMBID, item.MediaFileID)
			transition := f.transitions[key]
			transition.UserID, transition.SourceKey, transition.TargetKey = userID, item.TransitionSourceKey, model.RadioTrackKey(item.RecordingMBID, item.MediaFileID)
			switch delta {
			case "attempt":
				transition.AttemptCount++
			case "accepted":
				transition.AcceptedCount++
			case "completed":
				transition.CompletedCount++
			case "early":
				transition.EarlySkipCount++
			case "neutral":
				transition.NeutralSkipCount++
			case "keep":
				transition.KeepCount++
			}
			transition.UpdatedAt = now
			f.transitions[key] = transition
		}
		return &model.RadioPlaybackFeedbackResult{Item: *item, Applied: applied}, nil
	}
	return nil, model.ErrNotFound
}

func (f *fakePersonalRadioRepository) latestAccepted(sessionID, excludeID string) *model.PersonalRadioItem {
	for i := len(f.items) - 1; i >= 0; i-- {
		if f.items[i].SessionID == sessionID && f.items[i].ID != excludeID && model.IsAcceptedRadioPlaybackOutcome(f.items[i].PlaybackOutcome) {
			item := f.items[i]
			return &item
		}
	}
	return nil
}

func (f *fakePersonalRadioRepository) GetTransitionsForTargets(_ string, sourceKey string, targetKeys []string) (map[string]model.RadioTransitionFeedback, error) {
	result := map[string]model.RadioTransitionFeedback{}
	for _, targetKey := range targetKeys {
		if feedback, ok := f.transitions[sourceKey+"\x00"+targetKey]; ok {
			result[targetKey] = feedback
		}
	}
	return result, nil
}

func (f *fakePersonalRadioRepository) GetTopTransitions(_ string, sourceKey string, limit int) ([]model.RadioTransitionFeedback, error) {
	var result []model.RadioTransitionFeedback
	for _, feedback := range f.transitions {
		if feedback.SourceKey == sourceKey {
			result = append(result, feedback)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func minInt64ForTest(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
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

func (f *fakePersonalRadioRepository) GetDiscoveryByRecording(_ string, recordingMBID string) (*model.DiscoveryTrack, error) {
	if track, ok := f.discoveries[recordingMBID]; ok {
		copy := track
		return &copy, nil
	}
	return nil, model.ErrNotFound
}

func (f *fakePersonalRadioRepository) UpdateDiscovery(track *model.DiscoveryTrack) error {
	if f.discoveries == nil {
		f.discoveries = map[string]model.DiscoveryTrack{}
	}
	f.discoveries[track.RecordingMBID] = *track
	return nil
}

func (f *fakePersonalRadioRepository) RecordFeedback(_ string, recordingMBID, event string, _ time.Time) error {
	f.feedbackEvents = append(f.feedbackEvents, recordingMBID+":"+event)
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

func TestFeedbackPersistsLibraryOutcomeAndKeepsDiscoveryLifecycle(t *testing.T) {
	repo := &fakePersonalRadioRepository{
		session: &model.PersonalRadioSession{ID: "session", UserID: "user", Status: model.PersonalRadioActive},
		items: []model.PersonalRadioItem{
			{ID: "library-item", SessionID: "session", ItemType: model.RadioItemLibrary, RecordingMBID: "LIB-MBID"},
			{ID: "discovery-item", SessionID: "session", ItemType: model.RadioItemDiscovery, RecordingMBID: "DISC-MBID"},
		},
		discoveries: map[string]model.DiscoveryTrack{
			"disc-mbid": {ID: "discovery", UserID: "user", RecordingMBID: "disc-mbid", State: model.DiscoveryTemporary},
		},
	}
	svc := &service{repo: repo}
	if err := svc.Feedback(context.Background(), "user", "session", model.PersonalRadioFeedbackRequest{
		ItemID: "library-item", Event: model.RadioFeedbackThresholdReached, ListenedMS: 30000, DurationMS: 100000,
	}); err != nil {
		t.Fatal(err)
	}
	if repo.items[0].PlaybackOutcome != model.RadioPlaybackAccepted {
		t.Fatalf("library playback outcome = %q, want accepted", repo.items[0].PlaybackOutcome)
	}
	if len(repo.feedbackEvents) != 1 || repo.feedbackEvents[0] != "lib-mbid:threshold_reached" {
		t.Fatalf("library feedback events = %v", repo.feedbackEvents)
	}

	if err := svc.Feedback(context.Background(), "user", "session", model.PersonalRadioFeedbackRequest{
		ItemID: "discovery-item", Event: model.RadioFeedbackStarted,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Feedback(context.Background(), "user", "session", model.PersonalRadioFeedbackRequest{
		ItemID: "discovery-item", Event: model.RadioFeedbackThresholdReached, ListenedMS: 30000, DurationMS: 100000,
	}); err != nil {
		t.Fatal(err)
	}
	if got := repo.discoveries["disc-mbid"].State; got != model.DiscoveryKept {
		t.Fatalf("discovery state = %q, want kept", got)
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

func TestBuildLocalFallbackFeaturesDistinguishesSameGenreTracks(t *testing.T) {
	now := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	seed := &model.MediaFile{
		ID:       "seed",
		ArtistID: "seed-artist",
		Artist:   "Seed Artist",
		AlbumID:  "seed-album",
		Genre:    "Pop",
		Year:     2020,
	}
	closeCandidate := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: model.MediaFile{
		ID:       "close",
		ArtistID: "seed-artist",
		Artist:   "Seed Artist",
		AlbumID:  "other-album",
		Genre:    "Pop",
		Year:     2020,
	}}, []radioSeed{{File: seed, Weight: 1}}, string(model.RadioModeBalanced), now, 0)
	distantCandidate := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: model.MediaFile{
		ID:       "distant",
		ArtistID: "other-artist",
		Artist:   "Other Artist",
		AlbumID:  "other-album",
		Genre:    "Pop",
		Year:     1980,
		Annotations: model.Annotations{
			PlayCount: 100,
			PlayDate:  personalRadioTimePtr(now.Add(-time.Hour)),
		},
	}}, []radioSeed{{File: seed, Weight: 1}}, string(model.RadioModeBalanced), now, 0)

	if closeCandidate.SeedSimilarity <= distantCandidate.SeedSimilarity {
		t.Fatalf("close seed similarity = %v, distant = %v", closeCandidate.SeedSimilarity, distantCandidate.SeedSimilarity)
	}
	if closeCandidate.GenreOverlap != distantCandidate.GenreOverlap {
		t.Fatalf("same-genre overlap differs: close = %v, distant = %v", closeCandidate.GenreOverlap, distantCandidate.GenreOverlap)
	}
	if closeCandidate.Freshness <= distantCandidate.Freshness {
		t.Fatalf("close freshness = %v, distant = %v", closeCandidate.Freshness, distantCandidate.Freshness)
	}
}

func TestBuildLocalFallbackFeaturesUsesCurrentSessionSeed(t *testing.T) {
	now := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	original := &model.MediaFile{ID: "original", ArtistID: "original-artist", Artist: "Original Artist", Genre: "Pop", Year: 2020}
	current := &model.MediaFile{ID: "current", ArtistID: "current-artist", Artist: "Current Artist", Genre: "Jazz", Year: 2022}
	seeds := []radioSeed{{File: original, Weight: 0.35}, {File: current, Weight: 0.65}}

	currentMatch := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: model.MediaFile{ID: "current-match", ArtistID: "current-artist", Artist: "Current Artist", Genre: "Jazz", Year: 2022}}, seeds, string(model.RadioModeBalanced), now, 0)
	originalMatch := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: model.MediaFile{ID: "original-match", ArtistID: "original-artist", Artist: "Original Artist", Genre: "Pop", Year: 2020}}, seeds, string(model.RadioModeBalanced), now, 0)
	if currentMatch.SeedSimilarity <= originalMatch.SeedSimilarity {
		t.Fatalf("current-session seed similarity = %v, original-only = %v", currentMatch.SeedSimilarity, originalMatch.SeedSimilarity)
	}
	if currentMatch.GenreOverlap <= originalMatch.GenreOverlap {
		t.Fatalf("current-session genre overlap = %v, original-only = %v", currentMatch.GenreOverlap, originalMatch.GenreOverlap)
	}
}

func TestBuildLocalFallbackFeaturesChangesDiscoveryPreferenceByMode(t *testing.T) {
	now := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	seed := &model.MediaFile{ID: "seed", Artist: "Seed Artist", Genre: "Pop", Year: 2020}
	unheard := model.MediaFile{ID: "unheard", Artist: "New Artist", Genre: "Pop", Year: 2020, CreatedAt: now.Add(-24 * time.Hour)}
	familiar := model.MediaFile{
		ID: "familiar", Artist: "Known Artist", Genre: "Pop", Year: 2020,
		Annotations: model.Annotations{PlayCount: 100, PlayDate: personalRadioTimePtr(now.Add(-30 * 24 * time.Hour))},
		CreatedAt:   now.Add(-2 * 365 * 24 * time.Hour),
	}

	discoverUnheard := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: unheard}, []radioSeed{{File: seed, Weight: 1}}, string(model.RadioModeDiscover), now, 0)
	discoverFamiliar := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: familiar}, []radioSeed{{File: seed, Weight: 1}}, string(model.RadioModeDiscover), now, 0)
	familiarUnheard := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: unheard}, []radioSeed{{File: seed, Weight: 1}}, string(model.RadioModeFamiliar), now, 0)
	familiarKnown := buildLocalFallbackFeatures(recommendations.Candidate{MediaFile: familiar}, []radioSeed{{File: seed, Weight: 1}}, string(model.RadioModeFamiliar), now, 0)

	if discoverUnheard.DiscoveryPreference <= discoverFamiliar.DiscoveryPreference {
		t.Fatalf("discover preferences = unheard:%v familiar:%v", discoverUnheard.DiscoveryPreference, discoverFamiliar.DiscoveryPreference)
	}
	if familiarKnown.DiscoveryPreference <= familiarUnheard.DiscoveryPreference {
		t.Fatalf("familiar preferences = known:%v unheard:%v", familiarKnown.DiscoveryPreference, familiarUnheard.DiscoveryPreference)
	}
}

func personalRadioTimePtr(value time.Time) *time.Time {
	return &value
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
		ds:   ds,
		repo: &fakePersonalRadioRepository{},
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

func TestRecommendationPoolsRankLocalFallbacksByRelevance(t *testing.T) {
	now := time.Now().UTC()
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", ArtistID: "seed-artist", Artist: "Seed Artist", Genre: "Pop", Year: 2020, MbzRecordingID: "seed-mbid"},
		{ID: "close", ArtistID: "seed-artist", Artist: "Seed Artist", Genre: "Pop", Year: 2020, MbzRecordingID: "close-mbid"},
		{ID: "popular", ArtistID: "popular-artist", Artist: "Popular Artist", Genre: "Pop", Year: 2020, MbzRecordingID: "popular-mbid", Annotations: model.Annotations{
			PlayCount: 100,
			PlayDate:  personalRadioTimePtr(now.Add(-time.Hour)),
		}},
		{ID: "unrelated", ArtistID: "metal-artist", Artist: "Metal Artist", Genre: "Metal", Year: 1980, MbzRecordingID: "unrelated-mbid"},
	})
	repo := &fakePersonalRadioRepository{}
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: mediaRepo}, repo: repo}

	pools, err := svc.recommendationPools(
		context.Background(),
		model.PersonalRadioSession{ID: "session", UserID: "user", Mode: model.RadioModeBalanced},
		mediaRepo.Data["seed"],
		map[string]bool{"seed": true},
		nil,
		3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.local) != 3 {
		t.Fatalf("local fallback pool = %#v, want three playable candidates", pools.local)
	}
	for _, candidate := range pools.ranked {
		if candidate.source == "tasteFallback" || candidate.source == "exhaustiveFallback" {
			if candidate.candidate.LocalFallback == nil {
				t.Fatalf("fallback candidate %q has no local feature vector", candidate.candidate.Key)
			}
		}
	}

	selected := composeRadioCandidates(pools.ranked, radioCompositionOptions{Mode: string(model.RadioModeBalanced), Slots: 3})
	if len(selected) != 3 {
		t.Fatalf("selected fallback candidates = %#v, want three", selected)
	}
	if selected[0].candidate.MediaFile.ID != "close" || selected[1].candidate.MediaFile.ID != "popular" || selected[2].candidate.MediaFile.ID != "unrelated" {
		t.Fatalf("selected fallback order = [%s %s %s], want [close popular unrelated]", selected[0].candidate.MediaFile.ID, selected[1].candidate.MediaFile.ID, selected[2].candidate.MediaFile.ID)
	}
}

func TestRecommendationPoolsApplyLocalFallbackFeedbackAndTransitions(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Seed Artist", Genre: "Pop", Year: 2020, MbzRecordingID: "seed-mbid"},
		{ID: "fatigued", Artist: "Related Artist", Genre: "Pop", Year: 2020, MbzRecordingID: "fatigued-mbid"},
		{ID: "fresh", Artist: "Fresh Artist", Genre: "Pop", Year: 2020, MbzRecordingID: "fresh-mbid"},
	})
	repo := &fakePersonalRadioRepository{
		feedback: map[string]model.RadioTrackFeedback{
			"fatigued-mbid": {EarlySkipCount: 5},
		},
		transitions: map[string]model.RadioTransitionFeedback{
			"mbid:seed-mbid\x00mbid:fresh-mbid": {
				SourceKey: "mbid:seed-mbid", TargetKey: "mbid:fresh-mbid", AttemptCount: 4, AcceptedCount: 4,
			},
		},
	}
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: mediaRepo}, repo: repo}

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
	byID := make(map[string]rankedRadioCandidate, len(pools.ranked))
	for _, candidate := range pools.ranked {
		byID[candidate.candidate.MediaFile.ID] = candidate
	}
	fatigued := byID["fatigued"]
	fresh := byID["fresh"]
	if fatigued.candidate.LocalFallback == nil || fresh.candidate.LocalFallback == nil {
		t.Fatalf("fallback features missing: fatigued=%#v fresh=%#v", fatigued.candidate.LocalFallback, fresh.candidate.LocalFallback)
	}
	if fatigued.candidate.LocalFallback.FatiguePenalty <= fresh.candidate.LocalFallback.FatiguePenalty {
		t.Fatalf("fatigue penalties = fatigued:%v fresh:%v", fatigued.candidate.LocalFallback.FatiguePenalty, fresh.candidate.LocalFallback.FatiguePenalty)
	}
	if fresh.candidate.TransitionAffinity <= 0 || fresh.candidate.LocalFallback.TransitionRelevance <= 0 {
		t.Fatalf("fresh transition affinity/features = %v/%v, want positive", fresh.candidate.TransitionAffinity, fresh.candidate.LocalFallback.TransitionRelevance)
	}
	if fresh.ranked.Breakdown.LocalFallbackTransitionRelevance <= 0 {
		t.Fatalf("fresh transition score contribution = %v, want positive", fresh.ranked.Breakdown.LocalFallbackTransitionRelevance)
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

func TestRecommendationPoolsInjectStrongLearnedTransition(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed", Artist: "Seed Artist", Genre: "Pop", MbzRecordingID: "seed-mbid"},
		{ID: "learned", Title: "Learned", Artist: "Jazz Artist", Genre: "Jazz", MbzRecordingID: "learned-mbid"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{transitions: map[string]model.RadioTransitionFeedback{
		"mbid:seed-mbid\x00mbid:learned-mbid": {
			UserID: "user", SourceKey: "mbid:seed-mbid", TargetKey: "mbid:learned-mbid",
			TargetMediaFileID: "learned", AttemptCount: 4, AcceptedCount: 3, CompletedCount: 1,
		},
	}}
	svc := &service{
		ds:      ds,
		repo:    repo,
		agents:  fakeSimilarityProvider{},
		matcher: matcher.New(ds),
	}
	pools, err := svc.recommendationPools(
		context.Background(),
		model.PersonalRadioSession{ID: "session", UserID: "user"},
		mediaRepo.Data["seed"],
		map[string]bool{"seed": true},
		map[string]bool{"seed-mbid": true},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.local) != 1 || pools.local[0].ID != "learned" {
		t.Fatalf("learned local pool = %#v, want learned", pools.local)
	}
	if pools.ranked[0].candidate.TransitionAffinity <= 0 {
		t.Fatalf("learned transition affinity = %v, want positive", pools.ranked[0].candidate.TransitionAffinity)
	}
}

func TestRecommendationPoolsUseAnyEligibleLocalTrackAsLastFallback(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "unrelated", Title: "Unrelated", Artist: "Other Artist", Genre: "Metal"},
	})
	svc := &service{
		ds:   &tests.MockDataStore{MockedMediaFile: mediaRepo},
		repo: &fakePersonalRadioRepository{},
	}

	pools, err := svc.recommendationPools(
		context.Background(),
		model.PersonalRadioSession{ID: "session", UserID: "user"},
		mediaRepo.Data["seed"],
		map[string]bool{"seed": true},
		nil,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.local) != 1 || pools.local[0].ID != "unrelated" {
		t.Fatalf("local fallback = %#v, want unrelated", pools.local)
	}
}

func TestRecommendationPoolsSkipUnreadableLocalTracks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "playable.mp3"), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "missing", Title: "Missing", Artist: "Other Artist", LibraryPath: root, Path: "missing.mp3"},
		{ID: "playable", Title: "Playable", Artist: "Other Artist", LibraryPath: root, Path: "playable.mp3"},
	})
	svc := &service{
		ds:   &tests.MockDataStore{MockedMediaFile: mediaRepo},
		repo: &fakePersonalRadioRepository{},
	}

	pools, err := svc.recommendationPools(
		context.Background(),
		model.PersonalRadioSession{ID: "session", UserID: "user"},
		mediaRepo.Data["seed"],
		map[string]bool{"seed": true},
		nil,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.local) != 1 || pools.local[0].ID != "playable" {
		t.Fatalf("local fallback = %#v, want playable", pools.local)
	}
}

type pagedMediaFileRepository struct {
	model.MediaFileRepository
	files model.MediaFiles
}

func (r *pagedMediaFileRepository) GetAll(options ...model.QueryOptions) (model.MediaFiles, error) {
	max, offset := len(r.files), 0
	if len(options) > 0 {
		if options[0].Max > 0 && options[0].Max < max {
			max = options[0].Max
		}
		offset = options[0].Offset
	}
	if offset >= len(r.files) {
		return nil, nil
	}
	end := min(offset+max, len(r.files))
	return append(model.MediaFiles(nil), r.files[offset:end]...), nil
}

func TestExhaustiveLocalFallbackSearchesPastFirstPage(t *testing.T) {
	files := make(model.MediaFiles, 0, localFallbackPageSize+2)
	files = append(files, model.MediaFile{ID: "seed", Title: "Seed", Artist: "Seed Artist", Genre: "Pop"})
	for i := 0; i < localFallbackPageSize; i++ {
		files = append(files, model.MediaFile{ID: fmt.Sprintf("missing-%03d", i), Missing: true})
	}
	files = append(files, model.MediaFile{ID: "last-playable", Title: "Last", Artist: "Other Artist"})
	repo := &pagedMediaFileRepository{files: files}
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}

	candidates, stats, err := svc.localCandidateFilesForFallback(
		context.Background(),
		&files[0],
		map[string]bool{"seed": true},
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "last-playable" || stats.pages < 2 {
		t.Fatalf("candidates = %#v, stats = %#v, want last-playable after two pages", candidates, stats)
	}
}

func TestStatusForReadyItemsIncludesLibraryTracks(t *testing.T) {
	items := []model.PersonalRadioItem{{ItemType: model.RadioItemSeed, Status: model.RadioItemReady}, {
		ItemType: model.RadioItemLibrary,
		Status:   model.RadioItemReady,
	}}
	if status := statusForReadyItems(items); status != model.RadioPlanningReady {
		t.Fatalf("status = %q, want ready", status)
	}
	if status := statusForReadyItems(nil); status != model.RadioPlanningExhausted {
		t.Fatalf("empty status = %q, want exhausted", status)
	}
}

func TestBuildRadioContextKeepsOriginalAndAcceptedSeeds(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{
		{ID: "a", Title: "A", Artist: "Artist A", Genre: "Pop", MbzRecordingID: "a-mbid"},
		{ID: "b", Title: "B", Artist: "Artist B", Genre: "Pop", MbzRecordingID: "b-mbid"},
		{ID: "c", Title: "C", Artist: "Artist C", Genre: "Rock", MbzRecordingID: "c-mbid"},
		{ID: "x", Title: "X", Artist: "Artist X", Genre: "Metal", MbzRecordingID: "x-mbid"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{items: []model.PersonalRadioItem{
		{ID: "seed-item", SessionID: "session", ItemType: model.RadioItemSeed, MediaFileID: "a", PlaybackOutcome: model.RadioPlaybackAccepted},
		{ID: "b-item", SessionID: "session", ItemType: model.RadioItemLibrary, MediaFileID: "b", PlaybackOutcome: model.RadioPlaybackAccepted},
		{ID: "c-item", SessionID: "session", ItemType: model.RadioItemLibrary, MediaFileID: "c", PlaybackOutcome: model.RadioPlaybackCompleted},
		{ID: "x-item", SessionID: "session", ItemType: model.RadioItemLibrary, MediaFileID: "x", PlaybackOutcome: model.RadioPlaybackEarlySkip},
	}}
	svc := &service{ds: ds, repo: repo}
	context, err := svc.buildRadioContext(context.Background(), model.PersonalRadioSession{
		ID: "session", SeedMediaFileID: "a", UserID: "user",
	}, repo.items, model.RefillPersonalRadioRequest{
		CurrentItemID: "c-item", QueuedItemIDs: []string{"c-item", "x-item"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Seeds) != 3 {
		t.Fatalf("context seeds = %#v, want original/current/recent accepted", context.Seeds)
	}
	keys := make([]string, 0, len(context.Seeds))
	for _, seed := range context.Seeds {
		keys = append(keys, model.RadioTrackKey(seed.File.MbzRecordingID, seed.File.ID))
	}
	if got := strings.Join(keys, ","); got != "mbid:a-mbid,mbid:c-mbid,mbid:b-mbid" {
		t.Fatalf("context seed keys = %q", got)
	}
	if context.QueuedItemIDs["x-item"] != true || !context.ClientQueueProvided {
		t.Fatalf("client queue context was not reconciled: %#v", context)
	}
}

func TestBuildRadioContextDoesNotTreatEmptyClientQueueAsAuthoritative(t *testing.T) {
	mediaRepo := tests.CreateMockMediaFileRepo()
	mediaRepo.SetData(model.MediaFiles{{
		ID: "seed", Title: "Seed", Artist: "Seed Artist", MbzRecordingID: "seed-mbid",
	}})
	ds := &tests.MockDataStore{MockedMediaFile: mediaRepo}
	repo := &fakePersonalRadioRepository{}
	svc := &service{ds: ds, repo: repo}

	radioContext, err := svc.buildRadioContext(
		context.Background(),
		model.PersonalRadioSession{ID: "session", SeedMediaFileID: "seed", UserID: "user"},
		[]model.PersonalRadioItem{{
			ID: "seed-item", SessionID: "session", ItemType: model.RadioItemSeed,
			Status: model.RadioItemReady, MediaFileID: "seed",
		}},
		model.RefillPersonalRadioRequest{CurrentItemID: "seed-item", QueuedItemIDs: []string{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if radioContext.ClientQueueProvided {
		t.Fatal("empty client queue must fall back to server-side outstanding items")
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
	// Balanced composition targets roughly 65/35 over the refill and does not
	// force a discovery/library alternation.
	planned := repo.items[1:]
	if got := countRadioItemsByType(planned, model.RadioItemDiscovery); got != 4 {
		t.Fatalf("expected four balanced discovery items, got %d", got)
	}
	if got := countRadioItemsByType(planned, model.RadioItemLibrary); got != 6 {
		t.Fatalf("expected six balanced library items, got %d", got)
	}
	for i, item := range planned {
		if item.Position != i+1 {
			t.Fatalf("item %d has position %d, want %d", i, item.Position, i+1)
		}
	}
	if len(music.requests) != 4 {
		t.Fatalf("expected four discovery downloads, got %d", len(music.requests))
	}
	for _, request := range music.requests {
		if !strings.HasPrefix(request.ID, "fresh-") || request.Origin != model.MusicDownloadOriginRadio {
			t.Fatalf("unexpected discovery request %#v", request)
		}
	}
	// The Last.fm recommendation metadata rides on the download request so the
	// queue can show what is being fetched while the download is in flight.
	var freshOne *model.ExternalDownloadRequest
	for i := range music.requests {
		if music.requests[i].ID == "fresh-1" {
			freshOne = &music.requests[i]
			break
		}
	}
	if freshOne == nil || freshOne.Title != "Fresh One" || freshOne.Artist != "Fresh Artist" || freshOne.Album != "Fresh Album" {
		t.Fatalf("recommendation metadata missing from request %#v", music.requests)
	}

	// Polling while all ten slots are ready or downloading must not enqueue
	// another plan or flood the queue with library tracks.
	if err := svc.plan(context.Background(), session, mediaRepo.Data["seed"]); err != nil {
		t.Fatal(err)
	}
	if len(repo.items) != 11 || len(music.requests) != 4 {
		t.Fatalf("in-flight work was duplicated: %d items, %d downloads", len(repo.items), len(music.requests))
	}

	// A failed discovery no longer counts toward capacity. The next plan skips
	// its seen MBID and appends the next ranked discovery without requiring a
	// mechanical library pair.
	failedIndex := -1
	for index, item := range repo.items {
		if item.ItemType == model.RadioItemDiscovery {
			failedIndex = index
			break
		}
	}
	if failedIndex < 0 {
		t.Fatal("expected at least one discovery item")
	}
	repo.items[failedIndex].Status = model.RadioItemFailed
	if err := svc.plan(context.Background(), session, mediaRepo.Data["seed"]); err != nil {
		t.Fatal(err)
	}
	if len(repo.items) != 12 || len(music.requests) != 5 {
		t.Fatalf("expected one replacement item: %d items, %d downloads", len(repo.items), len(music.requests))
	}
	replacement := music.requests[4]
	if replacement.ID == "" || replacement.ID == "fresh-1" {
		t.Fatalf("expected failed discovery to advance to a new candidate, got %#v", replacement)
	}
	if repo.items[11].ItemType != model.RadioItemDiscovery || repo.items[11].Status != model.RadioItemDownloading {
		t.Fatalf("expected replacement discovery, got %#v", repo.items[11])
	}
}

func countRadioItemsByType(items []model.PersonalRadioItem, itemType string) int {
	count := 0
	for _, item := range items {
		if item.ItemType == itemType {
			count++
		}
	}
	return count
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

	response, err := svc.Refill(context.Background(), "user", "session", model.RefillPersonalRadioRequest{})
	if err != nil {
		t.Fatalf("Refill returned error: %v", err)
	}
	if repo.items[0].Status != model.RadioItemFailed {
		t.Fatalf("expected unmatched completed download to fail, got %q", repo.items[0].Status)
	}
	if response.PlanningStatus != model.RadioPlanningExhausted {
		t.Fatalf("expected exhausted status after failed completed download, got %q", response.PlanningStatus)
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

	response, err := svc.Refill(context.Background(), "user", "session", model.RefillPersonalRadioRequest{})
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
