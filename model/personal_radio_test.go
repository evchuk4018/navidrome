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
