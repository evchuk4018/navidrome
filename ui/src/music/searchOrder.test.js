import { getSearchSectionOrder, isSongTitleSearch } from './searchOrder'

const resultSet = ({ artists = [], songs = [] } = {}) => ({
  artists,
  albums: [],
  songs,
  genres: [],
})

describe('external music search section ordering', () => {
  it('prioritizes songs for an exact or phrase title match', () => {
    const results = resultSet({
      artists: [{ id: 'unrelated', name: 'The Boy That Got Away' }],
      songs: [
        {
          id: 'the-one-that-got-away',
          title: 'The One That Got Away',
          artistName: 'Katy Perry',
        },
      ],
    })

    expect(isSongTitleSearch('The one that got away', results)).toBe(true)
    expect(getSearchSectionOrder('The one that got away', results)).toEqual([
      'songs',
      'artists',
      'albums',
      'genres',
    ])
  })

  it('keeps artists first for an exact artist search', () => {
    const results = resultSet({
      artists: [{ id: 'katy-perry', name: 'Katy Perry' }],
      songs: [{ id: 'roar', title: 'Roar', artistName: 'Katy Perry' }],
    })

    expect(isSongTitleSearch('Katy Perry', results)).toBe(false)
    expect(getSearchSectionOrder('Katy Perry', results)).toEqual([
      'artists',
      'albums',
      'songs',
      'genres',
    ])
  })

  it('recognizes a title embedded in an artist-and-title query', () => {
    const results = resultSet({
      artists: [{ id: 'katy-perry', name: 'Katy Perry' }],
      songs: [
        {
          id: 'the-one-that-got-away',
          title: 'The One That Got Away',
          artistName: 'Katy Perry',
        },
      ],
    })

    expect(
      isSongTitleSearch('Katy Perry - The One That Got Away', results),
    ).toBe(true)
  })

  it('keeps the default order when no song title matches', () => {
    const results = resultSet({
      artists: [{ id: 'katy-perry', name: 'Katy Perry' }],
      songs: [{ id: 'roar', title: 'Roar', artistName: 'Katy Perry' }],
    })

    expect(getSearchSectionOrder('The One That Got Away', results)).toEqual([
      'artists',
      'albums',
      'songs',
      'genres',
    ])
  })

  it('keeps the default order for an empty or no-result search', () => {
    expect(getSearchSectionOrder('', resultSet())).toEqual([
      'artists',
      'albums',
      'songs',
      'genres',
    ])
  })
})
