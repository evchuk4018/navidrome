import { fireEvent, render, screen } from '@testing-library/react'
import ExternalArtwork from './ExternalArtwork'

describe('<ExternalArtwork />', () => {
  it('progresses through HTTPS candidates and ends with a placeholder', () => {
    render(
      <ExternalArtwork
        artworkUrls={[
          'http://untrusted.example/cover.jpg',
          'https://coverartarchive.org/first.jpg',
          'https://coverartarchive.org/second.jpg',
        ]}
        alt="Blinding Lights"
        className="artwork"
      />,
    )

    let image = screen.getByAltText('Blinding Lights')
    expect(image).toHaveAttribute(
      'src',
      'https://coverartarchive.org/first.jpg',
    )
    expect(image).toHaveAttribute('loading', 'lazy')
    expect(image).toHaveAttribute('decoding', 'async')

    fireEvent.error(image)
    image = screen.getByAltText('Blinding Lights')
    expect(image).toHaveAttribute(
      'src',
      'https://coverartarchive.org/second.jpg',
    )

    fireEvent.error(image)
    expect(
      screen.getByRole('img', { name: 'Blinding Lights' }),
    ).toHaveTextContent('B')
  })
})
