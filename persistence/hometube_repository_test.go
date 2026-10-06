package persistence

import (
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Mixed playlists", func() {
	It("persists ordered duplicate videos with music and retains them during music-only reads", func() {
		ctx := request.WithUser(GinkgoT().Context(), model.User{ID: "userid", IsAdmin: true})
		videos := NewHomeTubeRepository(ctx, GetDBXBuilder())
		video := &model.HomeTubeVideo{ID: "youtube-video", Title: "Video", ChannelID: "channel", ChannelName: "Channel", DurationSeconds: 90}
		Expect(videos.Put(video)).To(Succeed())
		repo := NewPlaylistRepository(ctx, GetDBXBuilder())
		playlist := &model.Playlist{Name: "Mixed", OwnerID: "userid"}
		Expect(repo.Put(playlist)).To(Succeed())
		tracks := repo.Tracks(playlist.ID, false)
		Expect(tracks.AddEntries([]model.PlaylistEntry{{Source: "music", ID: songDayInALife.ID}, {Source: "hometube", ID: video.ID}, {Source: "hometube", ID: video.ID}})).To(Equal(3))
		rows, err := tracks.GetAll(model.QueryOptions{Sort: "id"})
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(3))
		Expect(rows[1].Video.Title).To(Equal("Video"))
		Expect(rows[1].MediaFileID).To(BeEmpty())
		Expect(rows[1].Duration).To(Equal(float32(90)))
		Expect(tracks.Read("2")).To(Equal(&rows[1]))
		Expect(tracks.CountAll()).To(Equal(int64(3)))
		Expect(collectCursor(tracks.GetCursor(model.QueryOptions{Sort: "id"}))).To(Equal([]model.PlaylistTrack(rows)))
		saved, err := repo.GetWithTracks(playlist.ID, false, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(saved.HasVideos).To(BeTrue())
		Expect(saved.MediaFiles()).To(HaveLen(1))
		Expect(tracks.Reorder(3, 1)).To(Succeed())
		rows, err = tracks.GetAll(model.QueryOptions{Sort: "id"})
		Expect(err).NotTo(HaveOccurred())
		Expect(rows[0].Source).To(Equal("hometube"))
		Expect(tracks.Delete("1")).To(Succeed())
		Expect(tracks.CountAll()).To(Equal(int64(2)))
	})
	It("stores favorites per user without changing shared metadata", func() {
		ctx := request.WithUser(GinkgoT().Context(), model.User{ID: "userid", IsAdmin: true})
		repo := NewHomeTubeRepository(ctx, GetDBXBuilder())
		Expect(repo.Put(&model.HomeTubeVideo{ID: "favorite-video", Title: "A video"})).To(Succeed())
		Expect(repo.SetFavorite("favorite-video", true)).To(Succeed())
		saved, err := repo.Get("favorite-video")
		Expect(err).NotTo(HaveOccurred())
		Expect(saved.Starred).To(BeTrue())
		other := NewHomeTubeRepository(request.WithUser(ctx, model.User{ID: "other-user"}), GetDBXBuilder())
		saved, err = other.Get("favorite-video")
		Expect(err).NotTo(HaveOccurred())
		Expect(saved.Starred).To(BeFalse())
	})
})
