package persistence

import (
	"context"
	"time"

	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WebPlaybackSessionRepository", func() {
	It("fences radio creation and replaces active sessions in the same transaction", func() {
		ctx := request.WithUser(context.Background(), adminUser)
		web := NewWebPlaybackSessionRepository(ctx, GetDBXBuilder())
		radio := NewPersonalRadioRepository(db.Db()).(*personalRadioRepository)
		now := time.Now().UTC()
		create := func(id, client string, epoch int64) error {
			session := &model.PersonalRadioSession{ID: id, UserID: adminUser.ID,
				SeedMediaFileID: songDayInALife.ID, Status: model.PersonalRadioActive,
				Mode: model.RadioModeRelated, CreatedAt: now, UpdatedAt: now}
			return radio.CreateWebSession(session, []model.PersonalRadioItem{{
				ID: id + "-seed", SessionID: id, ItemType: model.RadioItemSeed,
				Status: model.RadioItemReady, MediaFileID: songDayInALife.ID, CreatedAt: now, UpdatedAt: now,
			}}, model.WebPlaybackGuard{ClientID: client, Epoch: epoch})
		}
		DeferCleanup(func() {
			for _, id := range []string{"web-radio-a", "web-radio-b", "web-radio-old"} {
				_, _ = GetDBXBuilder().NewQuery("DELETE FROM personal_radio_session WHERE id={:id}").
					Bind(map[string]any{"id": id}).Execute()
			}
		})
		Expect(web.Store(&model.WebPlaybackSession{UserID: adminUser.ID, OwnerClientID: "a", Epoch: 1})).To(Succeed())
		Expect(create("web-radio-a", "a", 1)).To(Succeed())
		Expect(web.Store(&model.WebPlaybackSession{UserID: adminUser.ID, OwnerClientID: "b", Epoch: 2})).To(Succeed())
		Expect(create("web-radio-b", "b", 2)).To(Succeed())
		Expect(create("web-radio-old", "a", 1)).To(MatchError(model.ErrPlaybackOwnership))
		session, err := radio.GetSessionForUser("web-radio-b", adminUser.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(session.Status).To(Equal(model.PersonalRadioActive))
		item, err := web.RadioItemForUser(adminUser.ID, "web-radio-b", "web-radio-b-seed")
		Expect(err).ToNot(HaveOccurred())
		Expect(item.MediaFileID).To(Equal(songDayInALife.ID))
		Expect(item.ItemType).To(Equal(model.RadioItemSeed))
		_, err = web.RadioItemForUser("other-user", "web-radio-b", "web-radio-b-seed")
		Expect(err).To(MatchError(model.ErrNotFound))
	})
	It("persists mixed occurrences and replaces the same user's snapshot", func() {
		ctx := request.WithUser(context.Background(), model.User{ID: "userid", UserName: "userid", IsAdmin: true})
		repo := NewWebPlaybackSessionRepository(ctx, GetDBXBuilder())
		s := &model.WebPlaybackSession{UserID: "userid", OwnerClientID: "a", Epoch: 1, Revision: 1,
			SelectedEntryID: "second", PositionMS: 2000, UpdatedAt: time.Now().UTC(),
			Entries: []model.WebPlaybackEntry{{EntryID: "first", Source: "music", ID: "1001"},
				{EntryID: "second", Source: "music", ID: "1001"},
				{EntryID: "video", Source: "hometube", ID: "v", Video: &model.HomeTubeVideo{ID: "v", Title: "Video"}}}}
		Expect(repo.Store(s)).To(Succeed())
		s.OwnerClientID = "b"
		s.Epoch = 2
		s.PositionMS = 7000
		Expect(repo.Store(s)).To(Succeed())
		actual, err := repo.Retrieve("userid")
		Expect(err).ToNot(HaveOccurred())
		Expect(actual.Entries).To(Equal(s.Entries))
		Expect(actual.SelectedEntryID).To(Equal("second"))
		Expect(actual.PositionMS).To(Equal(int64(7000)))
		Expect(actual.OwnerClientID).To(Equal("b"))
		_, err = repo.Retrieve("someone-else")
		Expect(err).To(MatchError(model.ErrNotFound))
	})
})
