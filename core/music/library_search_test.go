package music

import (
	"context"
	"errors"
	"testing"

	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/model"
)

type librarySearchStore struct {
	model.DataStore
	repo model.MediaFileRepository
}

func (s librarySearchStore) MediaFile(context.Context) model.MediaFileRepository { return s.repo }

type librarySearchRepo struct {
	model.MediaFileRepository
	files model.MediaFiles
	err   error
}

func (r librarySearchRepo) Search(string, ...model.QueryOptions) (model.MediaFiles, error) {
	return r.files, r.err
}

func (r librarySearchRepo) GetAll(...model.QueryOptions) (model.MediaFiles, error) {
	return r.files, nil
}

type unavailableSearchCatalog struct{ fakeCatalog }

func (unavailableSearchCatalog) Search(context.Context, string) (model.ExternalMusicSearch, error) {
	return model.ExternalMusicSearch{}, errors.New("catalog offline")
}

func TestMergeLibrarySearchMarksCatalogSongAndAddsLibraryOnlySong(t *testing.T) {
	owned := model.MediaFile{ID: "local-owned", Title: "Talking Body", Artist: "Tove Lo", Duration: 238}
	onlyLocal := model.MediaFile{ID: "local-only", Title: "Talking Body (Live)", Artist: "Tove Lo", Duration: 250}
	s := &service{ds: librarySearchStore{repo: librarySearchRepo{files: model.MediaFiles{owned, onlyLocal}}}}
	result, err := s.mergeLibrarySearch(context.Background(), "Talking Body", 30, model.ExternalMusicSearch{
		Songs: []model.ExternalTrack{{ID: "catalog-owned", Title: "Talking Body", ArtistName: "Tove Lo", Duration: 240}},
	})
	if err != nil {
		t.Fatalf("mergeLibrarySearch: %v", err)
	}
	if len(result.Songs) != 2 {
		t.Fatalf("got %d songs, want one merged and one library-only: %#v", len(result.Songs), result.Songs)
	}
	if got := result.Songs[0]; got.Source != "catalog" || got.LocalMediaFileID != owned.ID {
		t.Fatalf("catalog song was not marked downloaded: %#v", got)
	}
	if got := result.Songs[1]; got.Source != "library" || got.ID != onlyLocal.ID || got.LocalMediaFileID != onlyLocal.ID {
		t.Fatalf("library-only song was not included: %#v", got)
	}
}

func TestSearchReturnsLibrarySongsWhenCatalogIsUnavailable(t *testing.T) {
	store := librarySearchStore{repo: librarySearchRepo{files: model.MediaFiles{{
		ID: "local", Title: "Talking Body", Artist: "Tove Lo",
	}}}}
	s := NewWithLibrary(unavailableSearchCatalog{}, nil, nil, &fakeJobs{}, nil, nil, store, nil)
	result, err := s.Search(context.Background(), "user", "Talking Body", 30)
	if err != nil {
		t.Fatalf("Search should return local results: %v", err)
	}
	if !result.Partial || len(result.Results) != 1 || result.Results[0].Song == nil || result.Results[0].Song.Source != "library" {
		t.Fatalf("local fallback missing: %#v", result)
	}
}

func TestSearchMarksLibraryStatusUnavailableWhenLookupFails(t *testing.T) {
	store := librarySearchStore{repo: librarySearchRepo{err: errors.New("database unavailable")}}
	s := NewWithLibrary(fakeCatalog{search: model.ExternalMusicSearch{Songs: []model.ExternalTrack{{
		ID: "catalog", Title: "Talking Body", ArtistName: "Tove Lo",
	}}}}, nil, nil, &fakeJobs{}, nil, nil, store, nil)
	result, err := s.Search(context.Background(), "user", "Talking Body", 30)
	if err != nil {
		t.Fatalf("Search should retain catalog results: %v", err)
	}
	if !result.Partial || len(result.DegradedSources) != 1 || result.DegradedSources[0] != "library" || len(result.Results) != 1 {
		t.Fatalf("catalog partial result missing: %#v", result)
	}
}

func TestSameLibrarySongRequiresMatchingVersionAndDuration(t *testing.T) {
	base := model.ExternalTrack{ID: "catalog-recording", Title: "Talking Body", ArtistName: "Tove Lo", Duration: 240}
	file := model.MediaFile{ID: "local", Title: "Talking Body", Artist: "Tove Lo", Duration: 238}
	if !sameLibrarySong(base, file) {
		t.Fatal("exact title and artist with close duration should match")
	}
	file.Title = "Talking Body (Live)"
	if sameLibrarySong(base, file) {
		t.Fatal("live version should remain distinct")
	}
	file.Title = "Talking Body"
	file.Duration = 280
	if sameLibrarySong(base, file) {
		t.Fatal("different duration should remain distinct without an identifier match")
	}
	file.MbzRecordingID = base.ID
	if !sameLibrarySong(base, file) {
		t.Fatal("same recording ID should identify a song despite duration differences")
	}
	file.Missing = true
	if sameLibrarySong(base, file) {
		t.Fatal("missing library files must not be marked downloaded")
	}
}

func TestSameLibrarySongMatchesISRC(t *testing.T) {
	track := model.ExternalTrack{ID: "catalog-recording", Title: "Talking Body", ArtistName: "Tove Lo", ISRCs: []string{"USABC1234567"}}
	file := model.MediaFile{ID: "local", Title: "Different tag title", Artist: "Tove Lo", Tags: model.Tags{model.TagISRC: {"usabc1234567"}}}
	if !sameLibrarySong(track, file) {
		t.Fatal("matching ISRC should identify the library copy")
	}
}

func TestCreateManualDownloadRejectsSongAlreadyInLibrary(t *testing.T) {
	track := model.ExternalTrack{ID: "recording-owned", Title: "Talking Body", ArtistName: "Tove Lo"}
	store := librarySearchStore{repo: librarySearchRepo{files: model.MediaFiles{{
		ID: "local-owned", Title: "Talking Body", Artist: "Tove Lo", MbzRecordingID: track.ID,
	}}}}
	jobs := &fakeJobs{}
	s := NewWithLibrary(fakeCatalog{track: track}, nil, nil, jobs, nil, nil, store, matcher.New(store))
	_, err := s.CreateDownload(context.Background(), "user", model.ExternalDownloadRequest{Kind: model.MusicDownloadSong, ID: track.ID})
	if !errors.Is(err, model.ErrAlreadyDownloaded) {
		t.Fatalf("got %v, want already downloaded", err)
	}
	if jobs.job != nil {
		t.Fatalf("duplicate download was queued: %#v", jobs.job)
	}
}

func TestCreateManualDownloadAllowsDifferentVersion(t *testing.T) {
	track := model.ExternalTrack{ID: "recording-studio", Title: "Talking Body", ArtistName: "Tove Lo", Duration: 240}
	store := librarySearchStore{repo: librarySearchRepo{files: model.MediaFiles{{
		ID: "local-live", Title: "Talking Body (Live)", Artist: "Tove Lo", MbzRecordingID: track.ID,
	}}}}
	jobs := &fakeJobs{}
	s := NewWithLibrary(fakeCatalog{track: track}, nil, nil, jobs, nil, nil, store, matcher.New(store))
	job, err := s.CreateDownload(context.Background(), "user", model.ExternalDownloadRequest{Kind: model.MusicDownloadSong, ID: track.ID})
	if err != nil {
		t.Fatalf("distinct version should remain downloadable: %v", err)
	}
	if job == nil || jobs.job == nil {
		t.Fatal("distinct version was not queued")
	}
}
