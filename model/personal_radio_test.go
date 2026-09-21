package model

import "testing"

func TestRadioTrackKeyPrefersNormalizedRecordingMBID(t *testing.T) {
	if got := RadioTrackKey("  ABCD-1234  ", "media-1"); got != "mbid:abcd-1234" {
		t.Fatalf("RadioTrackKey() = %q, want mbid:abcd-1234", got)
	}
}

func TestRadioTrackKeyFallsBackToMediaFileID(t *testing.T) {
	if got := RadioTrackKey("", " media-1 "); got != "media:media-1" {
		t.Fatalf("RadioTrackKey() = %q, want media:media-1", got)
	}
	if got := RadioTrackKey("   ", ""); got != "" {
		t.Fatalf("RadioTrackKey() without identity = %q, want empty", got)
	}
}

func TestIsAcceptedRadioPlaybackOutcome(t *testing.T) {
	for _, outcome := range []string{RadioPlaybackAccepted, RadioPlaybackCompleted, RadioPlaybackLateSkip, RadioPlaybackKeep} {
		if !IsAcceptedRadioPlaybackOutcome(outcome) {
			t.Errorf("outcome %q should be an accepted context", outcome)
		}
	}
	if IsAcceptedRadioPlaybackOutcome(RadioPlaybackEarlySkip) {
		t.Error("early skip should not be an accepted context")
	}
}

func TestNormalizeRadioMode(t *testing.T) {
	if got := NormalizeRadioMode(" DISCOVER "); got != RadioModeDiscover {
		t.Fatalf("NormalizeRadioMode(discover) = %q, want %q", got, RadioModeDiscover)
	}
	if got := NormalizeRadioMode("unknown"); got != RadioModeBalanced {
		t.Fatalf("NormalizeRadioMode(unknown) = %q, want %q", got, RadioModeBalanced)
	}
}

func TestCreatePersonalRadioRequestRequiresExactlyOneSource(t *testing.T) {
	cases := []struct {
		name    string
		request CreatePersonalRadioRequest
		wantErr bool
	}{
		{name: "song", request: CreatePersonalRadioRequest{SeedMediaFileID: "song-1"}},
		{name: "playlist", request: CreatePersonalRadioRequest{SourcePlaylistID: "playlist-1"}},
		{name: "neither", wantErr: true},
		{name: "both", request: CreatePersonalRadioRequest{SeedMediaFileID: "song-1", SourcePlaylistID: "playlist-1"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.request.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestPersonalRadioResponseCarriesRevisionAndQueueSlices(t *testing.T) {
	response := PersonalRadioSessionResponse{
		Session:      PersonalRadioSession{Revision: 7},
		Revision:     7,
		UpNext:       []PersonalRadioItem{{ID: "ready"}},
		PendingItems: []PersonalRadioItem{{ID: "pending", Status: RadioItemDownloading}},
	}
	if response.Revision != response.Session.Revision {
		t.Fatalf("response revision %d did not mirror session revision %d", response.Revision, response.Session.Revision)
	}
	if len(response.UpNext) != 1 || len(response.PendingItems) != 1 {
		t.Fatalf("queue slices were not retained: %#v", response)
	}
}

func TestRadioFeedbackEventsIncludeTechnicalAndTasteSignals(t *testing.T) {
	if RadioFeedbackDislike == RadioFeedbackUnplayable {
		t.Fatal("dislike and unplayable must remain distinct signals")
	}
	if got := RadioTrackKey("", "local-only"); got != "media:local-only" {
		t.Fatalf("local-only feedback key = %q", got)
	}
}
