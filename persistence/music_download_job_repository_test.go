package persistence

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/navidrome/navidrome/model"
)

func TestFindOrCreatePlayPromotesQueuedJobAheadOfManualDownloads(t *testing.T) {
	repo := newMusicDownloadTestRepository(t)
	now := time.Now().UTC()
	ordinary := &model.MusicDownloadJob{
		ID: "ordinary", UserID: "user", Kind: model.MusicDownloadSong, SourceID: "other",
		Status: model.MusicDownloadQueued, Origin: model.MusicDownloadOriginManual,
		CreatedAt: now.Add(-time.Minute),
	}
	requested := &model.MusicDownloadJob{
		ID: "existing", UserID: "user", Kind: model.MusicDownloadSong, SourceID: "seed",
		Status: model.MusicDownloadQueued, Origin: model.MusicDownloadOriginManual,
		CreatedAt: now,
	}
	for _, job := range []*model.MusicDownloadJob{ordinary, requested} {
		if err := repo.Create(job); err != nil {
			t.Fatal(err)
		}
	}
	play := &model.MusicDownloadJob{
		ID: "new", UserID: "user", Kind: model.MusicDownloadSong, SourceID: "seed",
		Status: model.MusicDownloadQueued, Origin: model.MusicDownloadOriginManual,
		Priority: 200, CreatedAt: now.Add(time.Second),
	}
	reused, err := repo.FindOrCreatePlay(play)
	if err != nil || reused.ID != requested.ID || reused.Priority != 200 {
		t.Fatalf("play request did not promote existing job: %#v, %v", reused, err)
	}
	claimed, err := repo.ClaimNext(model.MusicDownloadOriginManual)
	if err != nil || claimed.ID != requested.ID {
		t.Fatalf("interactive job did not move to front: %#v, %v", claimed, err)
	}
	if claimed.Priority != 200 {
		t.Fatalf("claimed job priority = %d, want 200", claimed.Priority)
	}
	running, err := repo.FindOrCreatePlay(play)
	if err != nil || running.ID != requested.ID || running.Status != model.MusicDownloadRunning {
		t.Fatalf("running job was not reused: %#v, %v", running, err)
	}
	otherUser := *play
	otherUser.ID, otherUser.UserID = "other-user", "other"
	separate, err := repo.FindOrCreatePlay(&otherUser)
	if err != nil || separate.ID != otherUser.ID {
		t.Fatalf("another user's job was incorrectly reused: %#v, %v", separate, err)
	}
}

func newMusicDownloadTestRepository(t *testing.T) *musicDownloadJobRepository {
	t.Helper()
	database, err := sql.Open("sqlite3", "file:music-download-play-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec(`create table music_download_job (
		id text primary key, user_id text, kind text, source_id text,
		artist text not null default '', album text not null default '', title text not null default '',
		status text, message text not null default '', error text not null default '',
		output_path text not null default '', completed integer not null default 0,
		total integer not null default 0, created_at datetime, updated_at datetime,
		started_at datetime, finished_at datetime, origin text, priority integer not null default 0,
		radio_item_id text not null default '', media_file_id text not null default ''
	)`)
	if err != nil {
		t.Fatal(err)
	}
	return &musicDownloadJobRepository{db: database}
}
