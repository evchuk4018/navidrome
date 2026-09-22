package musicbrainz

import (
	"strings"
	"testing"
)

func TestLuceneEscapeHandlesReservedCharactersAndUnicode(t *testing.T) {
	escaped := luceneEscape(`Beyoncé (Live): "Halo" + remix?`)
	for _, expected := range []string{`Beyoncé`, `\(`, `\)`, `\:`, `\"`, `\+`, `\?`} {
		if !strings.Contains(escaped, expected) {
			t.Fatalf("escaped query %q does not contain %q", escaped, expected)
		}
	}
}

func TestRecordingQueryCombinesStrictTokenAndPrefixClauses(t *testing.T) {
	query := recordingQueryValues("The One That Got Away").Get("query")
	for _, expected := range []string{`recording:"The One That Got Away"`, `artist:The`, `Away*`} {
		if !strings.Contains(query, expected) {
			t.Fatalf("query %q does not contain %q", query, expected)
		}
	}
}

func TestFuzzyRecordingQueryIsBounded(t *testing.T) {
	query := recordingQueryValues("Blinding Ligths", true).Get("query")
	if !strings.Contains(query, "~1") || strings.Contains(query, "~2") {
		t.Fatalf("unexpected fuzzy query: %q", query)
	}
}
