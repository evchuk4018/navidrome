package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/model"
)

type fakeCatalog struct {
	track      model.ExternalTrack
	trackErr   error
	songSearch []model.ExternalTrack
}

func (f fakeCatalog) Search(context.Context, string) (model.ExternalMusicSearch, error) {
	return model.ExternalMusicSearch{}, nil
}

func (f fakeCatalog) Artist(context.Context, string) (model.ExternalArtistDetails, error) {
	return model.ExternalArtistDetails{}, nil
}

func (f fakeCatalog) Album(context.Context, string) (model.ExternalAlbumDetails, error) {
	return model.ExternalAlbumDetails{}, nil
}

func (f fakeCatalog) Recording(context.Context, string) (model.ExternalTrack, error) {
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
	service := New(
		fakeCatalog{
			trackErr: model.ErrNotFound,
			songSearch: []model.ExternalTrack{
				{ID: "recording-junk", Title: "Unrelated", ArtistName: "Someone Else"},
				{ID: "recording-resolved", Title: "Song", ArtistName: "Artist", AlbumTitle: "Album"},
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
