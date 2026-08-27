package agents

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Song.Equals", func() {
	base := Song{ID: "1", Name: "S", Artists: []Artist{{ID: "x", Name: "A"}}}
	It("true for identical songs incl Artists", func() {
		Expect(base.Equals(base)).To(BeTrue())
	})
	It("false when Artists differ", func() {
		other := base
		other.Artists = []Artist{{ID: "y", Name: "B"}}
		Expect(base.Equals(other)).To(BeFalse())
	})
	It("false when a scalar differs", func() {
		other := base
		other.Name = "T"
		Expect(base.Equals(other)).To(BeFalse())
	})
	It("true when both have empty Artists and equal scalars", func() {
		a := Song{ID: "1", Name: "S"}
		Expect(a.Equals(a)).To(BeTrue())
	})
	It("ignores recommendation metadata", func() {
		withMetadata := base
		withMetadata.CandidateID = "mbid:one"
		withMetadata.SimilarityScores = []SimilarityScore{{Provider: "lastfm", Score: 0.9, NormalizedScore: 0.9}}
		Expect(base.Equals(withMetadata)).To(BeTrue())
	})
})

var _ = Describe("CandidateID", func() {
	It("uses a normalized MBID when available", func() {
		Expect(CandidateID(Song{MBID: "  MBID-1  ", Name: "Different", Artists: []Artist{{Name: "Artist"}}})).To(Equal("mbid:mbid-1"))
	})

	It("falls back to normalized title and first artist", func() {
		Expect(CandidateID(Song{Name: "  Same   Song ", Artists: []Artist{{Name: "  Same   Artist "}}})).To(Equal("title:same song|artist:same artist"))
	})

	It("does not collapse distinct MBIDs with the same title and artist", func() {
		first := CandidateID(Song{MBID: "mbid-1", Name: "Song", Artists: []Artist{{Name: "Artist"}}})
		second := CandidateID(Song{MBID: "mbid-2", Name: "Song", Artists: []Artist{{Name: "Artist"}}})
		Expect(first).ToNot(Equal(second))
	})
})
