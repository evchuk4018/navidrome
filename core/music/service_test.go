package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

type fakeCatalog struct {
	track          model.ExternalTrack
	trackErr       error
	recordingByID  map[string]model.ExternalTrack
	recordingCalls *[]string
	songSearch     []model.ExternalTrack
	search         model.ExternalMusicSearch
}

func (f fakeCatalog) Search(context.Context, string) (model.ExternalMusicSearch, error) {
	return f.search, nil
}

func (f fakeCatalog) Artist(context.Context, string) (model.ExternalArtistDetails, error) {
	return model.ExternalArtistDetails{}, nil
}

func (f fakeCatalog) Album(context.Context, string) (model.ExternalAlbumDetails, error) {
	return model.ExternalAlbumDetails{}, nil
}

func (f fakeCatalog) Recording(_ context.Context, recordingID string) (model.ExternalTrack, error) {
	if f.recordingCalls != nil {
		*f.recordingCalls = append(*f.recordingCalls, recordingID)
	}
	if track, ok := f.recordingByID[recordingID]; ok {
		return track, nil
	}
	if f.trackErr != nil {
		return model.ExternalTrack{}, f.trackErr
	}
	return f.track, nil
}

func (f fakeCatalog) SearchSongs(context.Context, string) ([]model.ExternalTrack, error) {
	return f.songSearch, nil
}

type fakeDownloader struct{}

func (fakeDownloader) Download(_ context.Context, _ model.ExternalTrack, directory string) (string, error) {
	path := filepath.Join(directory, "track.mp3")
	return path, os.WriteFile(path, []byte("audio"), 0600)
}

type fakeTagger struct {
	files     []string
	metadata  model.ExternalTrack
	singleton bool
}

func (f *fakeTagger) Import(_ context.Context, files []string, metadata model.ExternalTrack, singleton bool) error {
	f.files = files
	f.metadata = metadata
	f.singleton = singleton
	return nil
}

type fakeJobs struct {
	job *model.MusicDownloadJob
}

func (f *fakeJobs) Create(job *model.MusicDownloadJob) error {
	clone := *job
	f.job = &clone
	return nil
}

func (f *fakeJobs) FindOrCreatePlay(job *model.MusicDownloadJob) (*model.MusicDownloadJob, error) {
	if active, err := f.PromoteActivePlay(job.UserID, job.SourceID, job.Priority); err != nil || active != nil {
		return active, err
	}
	if err := f.Create(job); err != nil {
		return nil, err
	}
	return f.job, nil
}

func (f *fakeJobs) PromoteActivePlay(userID, sourceID string, priority int) (*model.MusicDownloadJob, error) {
	if f.job != nil && f.job.UserID == userID && f.job.SourceID == sourceID &&
		(f.job.Status == model.MusicDownloadQueued || f.job.Status == model.MusicDownloadRunning) {
		if f.job.Status == model.MusicDownloadQueued && f.job.Priority < priority {
			f.job.Priority = priority
		}
		return f.job, nil
	}
	return nil, nil
}

func (f *fakeJobs) Get(string) (*model.MusicDownloadJob, error) { return f.job, nil }

func (f *fakeJobs) GetForUser(string, string) (*model.MusicDownloadJob, error) { return f.job, nil }

func (f *fakeJobs) GetAllForUser(string, int) ([]model.MusicDownloadJob, error) {
	if f.job == nil {
		return nil, nil
	}
	return []model.MusicDownloadJob{*f.job}, nil
}

func (f *fakeJobs) ClaimNext(...string) (*model.MusicDownloadJob, error) { return nil, nil }

func (f *fakeJobs) Update(job *model.MusicDownloadJob) error {
	clone := *job
	f.job = &clone
	return nil
}

func (f *fakeJobs) RequeueRunning() error { return nil }

type lookupOnlyAffinity struct{ calls int }

func (f *lookupOnlyAffinity) LookupAffinityForCandidates(_ string, candidates []recommendations.TasteCandidateIdentity) (map[string]recommendations.TasteAffinity, error) {
	f.calls++
	result := make(map[string]recommendations.TasteAffinity, len(candidates))
	for _, candidate := range candidates {
		result[candidate.Key] = recommendations.ComposeTasteAffinity(0, 1, 0, 0)
	}
	return result, nil
}

func TestSearchRanksMixedResultsAndUsesLookupOnlyAffinity(t *testing.T) {
	affinity := &lookupOnlyAffinity{}
	service := NewWithAffinity(fakeCatalog{search: model.ExternalMusicSearch{
		Artists: []model.ExternalArtist{{ID: "artist", Name: "Adele"}},
		Songs:   []model.ExternalTrack{{ID: "song", Title: "Hello", ArtistName: "Adele"}},
	}}, nil, nil, &fakeJobs{}, nil, affinity)

	result, err := service.Search(context.Background(), "user-1", "Adele Hello", 30)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if affinity.calls != 1 {
		t.Fatalf("affinity lookup calls = %d, want 1", affinity.calls)
	}
	if len(result.Results) != 2 || result.Results[0].Song == nil {
		t.Fatalf("unexpected mixed order: %#v", result.Results)
	}
	if len(result.Songs) != 1 || len(result.Artists) != 1 {
		t.Fatalf("compatibility arrays not derived: %#v", result)
	}
}

func TestCreateDownloadValidatesAndQueues(t *testing.T) {
	jobs := &fakeJobs{}
	service := New(fakeCatalog{}, fakeDownloader{}, &fakeTagger{}, jobs, nil)

	job, err := service.CreateDownload(context.Background(), "user-1", model.ExternalDownloadRequest{
		Kind:   model.MusicDownloadSong,
		ID:     "recording-1",
		Title:  "Fresh Track",
		Artist: "Fresh Artist",
		Album:  "Fresh Album",
	})
	if err != nil {
		t.Fatalf("CreateDownload returned error: %v", err)
	}
	if job.Status != model.MusicDownloadQueued || jobs.job == nil {
		t.Fatalf("expected queued job, got %#v", job)
	}
	if jobs.job.Title != "Fresh Track" || jobs.job.Artist != "Fresh Artist" || jobs.job.Album != "Fresh Album" {
		t.Fatalf("expected recommendation metadata on job, got %#v", jobs.job)
	}

	_, err = service.CreateDownload(context.Background(), "user-1", model.ExternalDownloadRequest{
		Kind: "playlist",
		ID:   "recording-1",
	})
	if !errors.Is(err, model.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestPlayNowPromotesAndReusesActiveSongDownload(t *testing.T) {
	jobs := &fakeJobs{}
	service := New(fakeCatalog{}, fakeDownloader{}, &fakeTagger{}, jobs, nil)
	request := model.ExternalDownloadRequest{Kind: model.MusicDownloadSong, ID: "recording-1", PlayNow: true}
	queued, err := service.CreateDownload(context.Background(), "user-1", request)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Priority != playNowDownloadPriority || queued.Origin != model.MusicDownloadOriginManual {
		t.Fatalf("interactive job was not prioritized: %#v", queued)
	}
	queued.Priority = 0
	reused, err := service.CreateDownload(context.Background(), "user-1", request)
	if err != nil || reused.ID != queued.ID || reused.Priority != playNowDownloadPriority {
		t.Fatalf("queued job was not promoted and reused: %#v, %v", reused, err)
	}
	reused.Status = model.MusicDownloadRunning
	reused.Priority = 0
	running, err := service.CreateDownload(context.Background(), "user-1", request)
	if err != nil || running.ID != queued.ID || running.Priority != 0 {
		t.Fatalf("running job should be reused without reprioritizing: %#v, %v", running, err)
	}
	_, err = service.CreateDownload(context.Background(), "user-1", model.ExternalDownloadRequest{
		Kind: model.MusicDownloadAlbum, ID: "album-1", PlayNow: true,
	})
	if !errors.Is(err, model.ErrValidation) {
		t.Fatalf("album playNow should fail validation, got %v", err)
	}
}

func TestPlayNowReusesActiveJobWhileCatalogIsUnavailable(t *testing.T) {
	jobs := &fakeJobs{job: &model.MusicDownloadJob{
		ID: "job-1", UserID: "user-1", Kind: model.MusicDownloadSong,
		SourceID: "recording-1", Status: model.MusicDownloadQueued,
	}}
	store := librarySearchStore{repo: &librarySearchRepo{}}
	service := NewWithLibrary(fakeCatalog{trackErr: errors.New("catalog unavailable")},
		nil, nil, jobs, nil, nil, store, matcher.New(store))
	job, err := service.CreateDownload(context.Background(), "user-1", model.ExternalDownloadRequest{
		Kind: model.MusicDownloadSong, ID: "recording-1", PlayNow: true,
	})
	if err != nil || job.ID != "job-1" || job.Priority != playNowDownloadPriority {
		t.Fatalf("active job should be reusable without catalog: %#v, %v", job, err)
	}
}

func TestGetDownloadResolvesSongAfterDelayedScan(t *testing.T) {
	repo := &librarySearchRepo{}
	store := librarySearchStore{repo: repo}
	jobs := &fakeJobs{job: &model.MusicDownloadJob{
		ID: "job-1", UserID: "user-1", Kind: model.MusicDownloadSong,
		SourceID: "recording-1", Title: "Seed Song",
		Status: model.MusicDownloadSuccess,
	}}
	service := NewWithLibrary(fakeCatalog{}, nil, nil, jobs, nil, nil, store, matcher.New(store))
	first, err := service.GetDownload(context.Background(), "user-1", "job-1")
	if err != nil || first.MediaFileID != "" {
		t.Fatalf("download should wait for library indexing: %#v, %v", first, err)
	}
	repo.files = model.MediaFiles{{
		ID: "local-1", MbzRecordingID: "recording-1", Title: "Seed Song", Artist: "Seed Artist",
	}}
	ready, err := service.GetDownload(context.Background(), "user-1", "job-1")
	if err != nil || ready.MediaFileID != "local-1" || jobs.job.MediaFileID != "local-1" {
		t.Fatalf("completed job did not resolve its local song: %#v, %v", ready, err)
	}
}

func TestProcessSongDownloadsTagsAndScans(t *testing.T) {
	old := conf.SnapshotConfig()
	defer old()
	conf.Server.CacheFolder = conf.NewDir(t.TempDir())

	tagger := &fakeTagger{}
	service := New(
		fakeCatalog{track: model.ExternalTrack{
			ID:         "recording-1",
			Title:      "Song",
			ArtistName: "Artist",
			AlbumTitle: "Album",
		}},
		fakeDownloader{},
		tagger,
		&fakeJobs{},
		nil,
	).(*service)
	job := &model.MusicDownloadJob{
		ID:       "job-1",
		Kind:     model.MusicDownloadSong,
		SourceID: "recording-1",
		Status:   model.MusicDownloadRunning,
	}
	if err := service.processDownload(context.Background(), job); err != nil {
		t.Fatalf("processDownload returned error: %v", err)
	}
	if job.Completed != 1 || job.Total != 1 {
		t.Fatalf("expected completed single-track job, got %#v", job)
	}
	if !tagger.singleton || len(tagger.files) != 1 || tagger.metadata.Title != "Song" {
		t.Fatalf("unexpected tagger call: %#v", tagger)
	}
}

func TestProcessSongFallsBackToCatalogSearchWhenRecordingMissing(t *testing.T) {
	old := conf.SnapshotConfig()
	defer old()
	conf.Server.CacheFolder = conf.NewDir(t.TempDir())

	tagger := &fakeTagger{}
	var recordingCalls []string
	service := New(
		fakeCatalog{
			trackErr:       model.ErrNotFound,
			recordingCalls: &recordingCalls,
			recordingByID: map[string]model.ExternalTrack{
				"recording-resolved": {
					Genre: "Rock",
				},
			},
			songSearch: []model.ExternalTrack{
				{ID: "recording-junk", Title: "Unrelated", ArtistName: "Someone Else"},
				{ID: "recording-resolved", Title: "Song", ArtistName: "Artist", AlbumTitle: "Album", AlbumID: "album-search", Duration: 201, Year: 2021},
			},
		},
		fakeDownloader{},
		tagger,
		&fakeJobs{},
		nil,
	).(*service)
	job := &model.MusicDownloadJob{
		ID:       "job-1",
		Kind:     model.MusicDownloadSong,
		SourceID: "recording-gone",
		Title:    "Song",
		Artist:   "Artist",
		Status:   model.MusicDownloadRunning,
	}
	if err := service.processDownload(context.Background(), job); err != nil {
		t.Fatalf("processDownload returned error: %v", err)
	}
	if job.Completed != 1 || job.Title != "Song" || job.Artist != "Artist" || job.Album != "Album" {
		t.Fatalf("expected search-resolved metadata, got %#v", job)
	}
	if tagger.metadata.ID != "recording-resolved" {
		t.Fatalf("expected the search result to be tagged, got %#v", tagger.metadata)
	}
	if tagger.metadata.Genre != "Rock" {
		t.Fatalf("expected selected recording lookup metadata to win, got %#v", tagger.metadata)
	}
	if tagger.metadata.AlbumID != "album-search" || tagger.metadata.Duration != 201 || tagger.metadata.Year != 2021 {
		t.Fatalf("expected search metadata to survive partial selected lookup, got %#v", tagger.metadata)
	}
	if len(recordingCalls) != 2 || recordingCalls[0] != "recording-gone" || recordingCalls[1] != "recording-resolved" {
		t.Fatalf("expected only the original and selected recording lookups, got %v", recordingCalls)
	}
}

func TestProcessSongSearchFallbackRetainsMetadataWhenSelectedLookupFails(t *testing.T) {
	old := conf.SnapshotConfig()
	defer old()
	conf.Server.CacheFolder = conf.NewDir(t.TempDir())

	tagger := &fakeTagger{}
	var recordingCalls []string
	service := New(
		fakeCatalog{
			trackErr:       model.ErrNotFound,
			recordingCalls: &recordingCalls,
			songSearch: []model.ExternalTrack{
				{ID: "recording-resolved", Title: "Song", ArtistName: "Artist", AlbumTitle: "Album", Genre: "Hip-Hop"},
			},
		},
		fakeDownloader{},
		tagger,
		&fakeJobs{},
		nil,
	).(*service)
	job := &model.MusicDownloadJob{
		ID:       "job-1",
		Kind:     model.MusicDownloadSong,
		SourceID: "recording-gone",
		Title:    "Song",
		Artist:   "Artist",
		Status:   model.MusicDownloadRunning,
	}
	if err := service.processDownload(context.Background(), job); err != nil {
		t.Fatalf("processDownload returned error: %v", err)
	}
	if tagger.metadata.Genre != "Hip-Hop" {
		t.Fatalf("expected search metadata to survive selected lookup failure, got %#v", tagger.metadata)
	}
	if len(recordingCalls) != 2 || recordingCalls[0] != "recording-gone" || recordingCalls[1] != "recording-resolved" {
		t.Fatalf("expected selected lookup after the original failure, got %v", recordingCalls)
	}
}

func TestProcessSongFailsWhenSearchFindsNoMatch(t *testing.T) {
	old := conf.SnapshotConfig()
	defer old()
	conf.Server.CacheFolder = conf.NewDir(t.TempDir())

	service := New(
		fakeCatalog{trackErr: model.ErrNotFound, songSearch: []model.ExternalTrack{
			{ID: "recording-junk", Title: "Unrelated", ArtistName: "Someone Else"},
		}},
		fakeDownloader{},
		&fakeTagger{},
		&fakeJobs{},
		nil,
	).(*service)
	job := &model.MusicDownloadJob{
		ID:       "job-1",
		Kind:     model.MusicDownloadSong,
		SourceID: "recording-gone",
		Title:    "Song",
		Artist:   "Artist",
		Status:   model.MusicDownloadRunning,
	}
	if err := service.processDownload(context.Background(), job); err == nil {
		t.Fatal("expected processDownload to fail when no recording can be resolved")
	}
}
