package events

import (
	"context"
	"testing"

	"github.com/navidrome/navidrome/model/request"
)

func TestPlaybackEventsReachAllUserTabs(t *testing.T) {
	b := &broker{}
	ctx := request.WithClientUniqueId(context.Background(), "shared-cookie")
	message := b.prepareMessage(ctx, &PlaybackSessionChanged{UserID: "u", Epoch: 2})
	for _, c := range []client{{userID: "u", clientUniqueId: "shared-cookie"}, {userID: "u", clientUniqueId: "other-tab"}} {
		if !b.shouldSend(message, c) {
			t.Fatal("private event suppressed for a same-user tab")
		}
	}
	if b.shouldSend(message, client{userID: "other"}) {
		t.Fatal("event leaked to another user")
	}
	empty := b.prepareMessage(ctx, &PlaybackSessionChanged{})
	if b.shouldSend(empty, client{userID: "u"}) {
		t.Fatal("missing recipient became a broadcast")
	}
}
