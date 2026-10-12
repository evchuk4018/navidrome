package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
)

type playbackMemoryRepo struct{ sessions map[string][]byte }

func (r *playbackMemoryRepo) Retrieve(user string) (*model.WebPlaybackSession, error) {
	value, ok := r.sessions[user]
	if !ok {
		return nil, model.ErrNotFound
	}
	var session model.WebPlaybackSession
	if err := json.Unmarshal(value, &session); err != nil {
		return nil, err
	}
	session.UserID = user
	return &session, nil
}
func (r *playbackMemoryRepo) Store(session *model.WebPlaybackSession) error {
	value, err := json.Marshal(session)
	if err != nil {
		return err
	}
	r.sessions[session.UserID] = value
	return nil
}
func (r *playbackMemoryRepo) RadioItemForUser(string, string, string) (*model.PersonalRadioItem, error) {
	return nil, model.ErrNotFound
}

type playbackMediaRepo struct {
	model.MediaFileRepository
	missing bool
}

func (r *playbackMediaRepo) GetWithParticipants(id string) (*model.MediaFile, error) {
	if id != "song" {
		return nil, model.ErrNotFound
	}
	return &model.MediaFile{ID: id, Title: "Song", Artist: "Artist", Missing: r.missing}, nil
}

type playbackTestStore struct {
	*tests.MockDataStore
	mu sync.Mutex
}

func (d *playbackTestStore) WithTxImmediate(block func(model.DataStore) error, _ ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return block(d)
}

func playbackTestAPI() (*Router, *playbackMemoryRepo, *playbackMediaRepo) {
	repo := &playbackMemoryRepo{sessions: map[string][]byte{}}
	media := &playbackMediaRepo{}
	ds := &playbackTestStore{MockDataStore: &tests.MockDataStore{MockedWebPlaybackSession: repo, MockedMediaFile: media}}
	return &Router{ds: ds}, repo, media
}
func playbackReq(api *Router, method, path, user, client string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if user != "" {
		req = req.WithContext(request.WithUser(context.Background(), model.User{ID: user, UserName: user}))
	}
	if client != "" {
		req.Header.Set("X-ND-Client-Unique-Id", client)
	}
	r := chi.NewRouter()
	api.addWebPlaybackRoute(r)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	return response
}
func playbackResponse(t *testing.T, r *httptest.ResponseRecorder) *model.WebPlaybackSession {
	t.Helper()
	if r.Code != 200 {
		t.Fatalf("status %d: %s", r.Code, r.Body.String())
	}
	var data struct {
		Session *model.WebPlaybackSession `json:"session"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data.Session
}
func playbackSample() *model.WebPlaybackSession {
	return &model.WebPlaybackSession{Entries: []model.WebPlaybackEntry{
		{EntryID: "one", Source: "music", ID: "song"},
		{EntryID: "video", Source: "hometube", ID: "vid", Video: &model.HomeTubeVideo{ID: "vid", Title: "Video", ChannelName: "Channel"}},
		{EntryID: "duplicate", Source: "music", ID: "song"},
	}, SelectedEntryID: "duplicate", PositionMS: 12345, State: "paused", Mode: "shufflePlay"}
}

func TestWebPlaybackHandoff(t *testing.T) {
	api, _, _ := playbackTestAPI()
	first := playbackResponse(t, playbackReq(api, "POST", "/playback-session/claim", "u", "phone",
		playbackClaim{Intent: "play", RequestID: "first", Snapshot: playbackSample()}))
	if first.Epoch != 1 || len(first.Entries) != 3 || first.Entries[2].Song == nil {
		t.Fatalf("bad mixed queue: %+v", first)
	}
	next := playbackResponse(t, playbackReq(api, "POST", "/playback-session/claim", "u", "desktop",
		playbackClaim{Intent: "resume", RequestID: "second", ExpectedEpoch: 1}))
	if next.Epoch != 2 || next.PositionMS != 12345 || next.SelectedEntryID != "duplicate" || next.Mode != "shufflePlay" {
		t.Fatalf("bad resume: %+v", next)
	}
	pos := int64(0)
	old := playbackReq(api, "PATCH", "/playback-session", "u", "phone", playbackCheckpoint{
		Epoch: 1, Sequence: 99, PositionMS: &pos, SelectedEntryID: "duplicate", State: "playing"})
	if old.Code != 409 {
		t.Fatalf("old owner accepted: %d", old.Code)
	}
	summary := playbackResponse(t, playbackReq(api, "GET", "/playback-session?summary=true", "u", "desktop", nil))
	if len(summary.Entries) != 0 || summary.CurrentEntry == nil || summary.PositionMS != 12345 {
		t.Fatalf("bad summary: %+v", summary)
	}
}

func TestWebPlaybackReceiptsAndCheckpointOrder(t *testing.T) {
	api, _, _ := playbackTestAPI()
	payload := playbackClaim{Intent: "play", RequestID: "receipt", Snapshot: playbackSample()}
	playbackResponse(t, playbackReq(api, "POST", "/playback-session/claim", "u", "a", payload))
	retry := playbackResponse(t, playbackReq(api, "POST", "/playback-session/claim", "u", "a", payload))
	if retry.Epoch != 1 {
		t.Fatal("duplicate claim advanced epoch")
	}
	for _, seq := range []int64{2, 1, 2} {
		pos := seq * 1000
		playbackResponse(t, playbackReq(api, "PATCH", "/playback-session", "u", "a",
			playbackCheckpoint{Epoch: 1, Sequence: seq, PositionMS: &pos, SelectedEntryID: "duplicate", State: "playing"}))
	}
	s := playbackResponse(t, playbackReq(api, "GET", "/playback-session", "u", "a", nil))
	if s.PositionMS != 2000 || s.Sequence != 2 {
		t.Fatalf("older checkpoint overwrote progress: %+v", s)
	}
	playbackResponse(t, playbackReq(api, "POST", "/playback-session/claim", "u", "b",
		playbackClaim{Intent: "resume", RequestID: "new", ExpectedEpoch: 1}))
	if r := playbackReq(api, "POST", "/playback-session/claim", "u", "a", payload); r.Code != 409 {
		t.Fatal("old receipt reclaimed ownership")
	}
}

func TestWebPlaybackIsolationAndMissingMedia(t *testing.T) {
	api, _, media := playbackTestAPI()
	playbackResponse(t, playbackReq(api, "POST", "/playback-session/claim", "u", "a",
		playbackClaim{Intent: "play", RequestID: "first", Snapshot: playbackSample()}))
	if s := playbackResponse(t, playbackReq(api, "GET", "/playback-session?userId=u", "other", "b", nil)); s != nil {
		t.Fatal("other user saw session")
	}
	if r := playbackReq(api, "GET", "/playback-session", "", "a", nil); r.Code != 401 {
		t.Fatal("anonymous read accepted")
	}
	media.missing = true
	s := playbackResponse(t, playbackReq(api, "GET", "/playback-session", "u", "a", nil))
	if len(s.Entries) != 3 || !s.Entries[2].Unavailable || s.PositionMS != 12345 {
		t.Fatal("missing item lost its occurrence or position")
	}
	snapshot := playbackSample()
	snapshot.Entries[0].ID = "inaccessible"
	if r := playbackReq(api, "POST", "/playback-session/claim", "other", "b",
		playbackClaim{Intent: "play", RequestID: "bad", Snapshot: snapshot}); r.Code != 400 {
		t.Fatal("inaccessible media accepted")
	}
}

func TestWebPlaybackConcurrentClaims(t *testing.T) {
	api, _, _ := playbackTestAPI()
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, client := range []string{"a", "b"} {
		wg.Go(func() {
			results <- playbackReq(api, "POST", "/playback-session/claim", "u", client,
				playbackClaim{Intent: "play", RequestID: client, Snapshot: playbackSample()}).Code
		})
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for status := range results {
		if status == 200 {
			success++
		}
		if status == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("claims: success=%d conflict=%d", success, conflict)
	}
}
