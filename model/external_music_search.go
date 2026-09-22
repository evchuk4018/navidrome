package model

// DeriveCompatibilityArrays rebuilds the temporary entity-specific arrays in
// exactly the same order as the mixed result stream.
func (s *ExternalMusicSearch) DeriveCompatibilityArrays() {
	s.Artists = nil
	s.Albums = nil
	s.Songs = nil
	s.Genres = nil
	for _, hit := range s.Results {
		switch {
		case hit.Artist != nil:
			s.Artists = append(s.Artists, *hit.Artist)
		case hit.Album != nil:
			s.Albums = append(s.Albums, *hit.Album)
		case hit.Song != nil:
			s.Songs = append(s.Songs, *hit.Song)
		case hit.Genre != nil:
			s.Genres = append(s.Genres, *hit.Genre)
		}
	}
	if s.Artists == nil {
		s.Artists = []ExternalArtist{}
	}
	if s.Albums == nil {
		s.Albums = []ExternalAlbum{}
	}
	if s.Songs == nil {
		s.Songs = []ExternalTrack{}
	}
	if s.Genres == nil {
		s.Genres = []ExternalGenre{}
	}
}
