package nativeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mixedAPIService struct {
	mockPlaylistsService
	entries  []model.PlaylistEntry
	videos   map[string]*model.HomeTubeVideo
	musicIDs []string
}

func (s *mixedAPIService) AddEntries(_ context.Context, _ string, entries []model.PlaylistEntry) (int, error) {
	s.entries = entries
	return len(entries), nil
}
func (s *mixedAPIService) AddTracks(_ context.Context, _ string, ids []string) (int, error) {
	s.musicIDs = ids
	return len(ids), nil
}
func (s *mixedAPIService) AddAlbums(context.Context, string, []string) (int, error)  { return 0, nil }
func (s *mixedAPIService) AddArtists(context.Context, string, []string) (int, error) { return 0, nil }
func (s *mixedAPIService) AddDiscs(context.Context, string, []model.DiscID) (int, error) {
	return 0, nil
}
func (s *mixedAPIService) SaveVideo(_ context.Context, v *model.HomeTubeVideo) error {
	s.videos[v.ID] = v
	return nil
}
func (s *mixedAPIService) GetVideo(_ context.Context, id string) (*model.HomeTubeVideo, error) {
	v := s.videos[id]
	if v == nil {
		return nil, model.ErrNotFound
	}
	return v, nil
}
func (s *mixedAPIService) SetVideoFavorite(_ context.Context, id string, starred bool) error {
	s.videos[id].Starred = starred
	return nil
}

var _ = Describe("Mixed playlist native API", func() {
	var svc *mixedAPIService
	var router http.Handler
	var token string
	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		conf.Server.SessionTimeout = time.Minute
		users := tests.CreateMockUserRepo()
		ds := &tests.MockDataStore{MockedUser: users, MockedProperty: &tests.MockedPropertyRepo{}}
		auth.Init(ds)
		user := model.User{ID: "mixed-api-user", UserName: "mixed-api-user", NewPassword: "password"}
		Expect(users.Put(&user)).To(Succeed())
		var err error
		token, err = auth.CreateToken(&user)
		Expect(err).NotTo(HaveOccurred())
		svc = &mixedAPIService{videos: map[string]*model.HomeTubeVideo{}}
		router = server.JWTVerifier(New(context.Background(), ds, nil, svc, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil))
	})
	send := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	It("accepts ordered typed entries and preserves the legacy IDs payload", func() {
		w := send("POST", "/playlist/mixed/tracks", `{"entries":[{"source":"music","id":"same"},{"source":"hometube","id":"same","video":{"id":"same","title":"Video"}},{"source":"hometube","id":"same"}]}`, true)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(svc.entries).To(HaveLen(3))
		Expect(svc.entries[1].Source).To(Equal("hometube"))
		w = send("POST", "/playlist/mixed/tracks", `{"ids":["song"]}`, true)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(svc.musicIDs).To(Equal([]string{"song"}))
		w = send("POST", "/playlist/mixed/tracks", `{"entries":[{"source":"hometube","id":"x"}],"ids":["song"]}`, true)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})
	It("authenticates catalog/favorite operations and returns cached metadata", func() {
		Expect(send("GET", "/hometubeVideo/video", "", false).Code).To(Equal(http.StatusUnauthorized))
		w := send("PUT", "/hometubeVideo/video", `{"id":"ignored","title":"Video","channelName":"Channel","durationSeconds":45}`, true)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(send("PUT", "/hometubeVideo/video/favorite", `{"starred":true}`, true).Code).To(Equal(http.StatusOK))
		w = send("GET", "/hometubeVideo/video", "", true)
		var video model.HomeTubeVideo
		Expect(json.Unmarshal(w.Body.Bytes(), &video)).To(Succeed())
		Expect(video.ID).To(Equal("video"))
		Expect(video.Starred).To(BeTrue())
		Expect(video.Title).To(Equal("Video"))
	})
})
