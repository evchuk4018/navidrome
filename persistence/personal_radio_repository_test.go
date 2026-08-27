package persistence

import (
	"time"

	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PersonalRadioRepository contextual feedback", func() {
	var (
		repo      model.PersonalRadioRepository
		sessionID = "radio-context-feedback-test"
	)

	BeforeEach(func() {
		repo = NewPersonalRadioRepository(db.Db())
		_, _ = GetDBXBuilder().NewQuery("delete from personal_radio_session where id = {:id}").
			Bind(map[string]any{"id": sessionID}).Execute()
		_, _ = GetDBXBuilder().NewQuery("delete from radio_transition_feedback where user_id = {:userID} and source_key like 'mbid:%'").
			Bind(map[string]any{"userID": adminUser.ID}).Execute()
		now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
		session := model.PersonalRadioSession{
			ID: sessionID, UserID: adminUser.ID, SeedMediaFileID: songDayInALife.ID,
			Mode: model.RadioModeBalanced, Status: model.PersonalRadioActive,
			CreatedAt: now, UpdatedAt: now,
		}
		items := []model.PersonalRadioItem{
			{ID: "radio-context-seed", SessionID: sessionID, Position: 0, ItemType: model.RadioItemSeed, Status: model.RadioItemReady, MediaFileID: songDayInALife.ID, RecordingMBID: "source-mbid", CreatedAt: now, UpdatedAt: now},
			{ID: "radio-context-x", SessionID: sessionID, Position: 1, ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, MediaFileID: songComeTogether.ID, RecordingMBID: "x-mbid", CreatedAt: now, UpdatedAt: now},
			{ID: "radio-context-y", SessionID: sessionID, Position: 2, ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, MediaFileID: songRadioactivity.ID, RecordingMBID: "y-mbid", CreatedAt: now, UpdatedAt: now},
			{ID: "radio-context-b", SessionID: sessionID, Position: 3, ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, MediaFileID: songAntenna.ID, RecordingMBID: "b-mbid", CreatedAt: now, UpdatedAt: now},
		}
		Expect(repo.CreateSession(&session, items)).To(Succeed())
	})

	AfterEach(func() {
		_, _ = GetDBXBuilder().NewQuery("delete from personal_radio_session where id = {:id}").
			Bind(map[string]any{"id": sessionID}).Execute()
		_, _ = GetDBXBuilder().NewQuery("delete from radio_transition_feedback where user_id = {:userID} and source_key like 'mbid:%'").
			Bind(map[string]any{"userID": adminUser.ID}).Execute()
	})

	It("keeps skip chains anchored to the last accepted item", func() {
		base := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
		step := 0
		feedback := func(itemID, event string, listened int64) *model.RadioPlaybackFeedbackResult {
			step++
			result, err := repo.RecordPlaybackFeedback(adminUser.ID, sessionID, model.PersonalRadioFeedbackRequest{
				ItemID: itemID, Event: event, ListenedMS: listened, DurationMS: 100000,
			}, base.Add(time.Duration(step)*time.Millisecond))
			Expect(err).ToNot(HaveOccurred())
			return result
		}

		feedback("radio-context-seed", model.RadioFeedbackStarted, 0)
		feedback("radio-context-seed", model.RadioFeedbackThresholdReached, 20000)
		feedback("radio-context-x", model.RadioFeedbackStarted, 0)
		feedback("radio-context-x", model.RadioFeedbackManualSkip, 1000)
		feedback("radio-context-y", model.RadioFeedbackStarted, 0)
		feedback("radio-context-y", model.RadioFeedbackManualSkip, 1000)
		feedback("radio-context-b", model.RadioFeedbackStarted, 0)
		feedback("radio-context-b", model.RadioFeedbackCompleted, 100000)

		transitions, err := repo.GetTransitionsForTargets(adminUser.ID, "mbid:source-mbid", []string{
			"mbid:x-mbid", "mbid:y-mbid", "mbid:b-mbid",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(transitions["mbid:x-mbid"]).To(SatisfyAll(
			HaveField("AttemptCount", Equal(1)), HaveField("EarlySkipCount", Equal(1)),
		))
		Expect(transitions["mbid:y-mbid"]).To(SatisfyAll(
			HaveField("AttemptCount", Equal(1)), HaveField("EarlySkipCount", Equal(1)),
		))
		Expect(transitions["mbid:b-mbid"]).To(SatisfyAll(
			HaveField("AttemptCount", Equal(1)), HaveField("CompletedCount", Equal(1)),
		))
		xToB, err := repo.GetTransitionsForTargets(adminUser.ID, "mbid:x-mbid", []string{"mbid:b-mbid"})
		Expect(err).ToNot(HaveOccurred())
		Expect(xToB).To(BeEmpty())
		yToB, err := repo.GetTransitionsForTargets(adminUser.ID, "mbid:y-mbid", []string{"mbid:b-mbid"})
		Expect(err).ToNot(HaveOccurred())
		Expect(yToB).To(BeEmpty())
		accepted, err := repo.GetRecentAcceptedItems(sessionID, 2)
		Expect(err).ToNot(HaveOccurred())
		Expect(accepted).To(HaveLen(2))
		Expect(accepted[0].ID).To(Equal("radio-context-b"))
		Expect(accepted[1].ID).To(Equal("radio-context-seed"))
	})

	It("makes playback updates idempotent and isolates users", func() {
		started := model.PersonalRadioFeedbackRequest{ItemID: "radio-context-seed", Event: model.RadioFeedbackStarted, DurationMS: 100000}
		first, err := repo.RecordPlaybackFeedback(adminUser.ID, sessionID, started, time.Now().UTC())
		Expect(err).ToNot(HaveOccurred())
		Expect(first.Applied).To(BeTrue())
		second, err := repo.RecordPlaybackFeedback(adminUser.ID, sessionID, started, time.Now().UTC().Add(time.Second))
		Expect(err).ToNot(HaveOccurred())
		Expect(second.Applied).To(BeFalse())

		_, err = repo.RecordPlaybackFeedback(adminUser.ID, sessionID, model.PersonalRadioFeedbackRequest{
			ItemID: "radio-context-seed", Event: model.RadioFeedbackThresholdReached, ListenedMS: 20000, DurationMS: 100000,
		}, time.Now().UTC())
		Expect(err).ToNot(HaveOccurred())
		_, err = repo.RecordPlaybackFeedback(adminUser.ID, sessionID, model.PersonalRadioFeedbackRequest{
			ItemID: "radio-context-seed", Event: model.RadioFeedbackCompleted, ListenedMS: 100000, DurationMS: 100000,
		}, time.Now().UTC())
		Expect(err).ToNot(HaveOccurred())
		_, err = repo.RecordPlaybackFeedback(adminUser.ID, sessionID, model.PersonalRadioFeedbackRequest{
			ItemID: "radio-context-seed", Event: model.RadioFeedbackManualSkip, ListenedMS: 100000, DurationMS: 100000,
		}, time.Now().UTC())
		Expect(err).ToNot(HaveOccurred())

		item, err := repo.GetItemForUser("radio-context-seed", adminUser.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(item).To(SatisfyAll(
			HaveField("PlaybackOutcome", Equal(model.RadioPlaybackCompleted)),
			HaveField("ListenedMS", Equal(int64(100000))),
			HaveField("DurationMS", Equal(int64(100000))),
		))
		transitions, err := repo.GetTransitionsForTargets("another-user", "mbid:source-mbid", []string{"mbid:b-mbid"})
		Expect(err).ToNot(HaveOccurred())
		Expect(transitions).To(BeEmpty())
	})

	It("persists and normalizes the session tuning mode", func() {
		session, err := repo.GetSessionForUser(sessionID, adminUser.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(session.Mode)).To(Equal(string(model.RadioModeBalanced)))
		session.Mode = " DISCOVER "
		Expect(repo.UpdateSession(session)).To(Succeed())
		updated, err := repo.GetSessionForUser(sessionID, adminUser.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(updated.Mode)).To(Equal(string(model.RadioModeDiscover)))
	})
})
