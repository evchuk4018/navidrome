import { useEffect, useMemo, useState } from 'react'
import PropTypes from 'prop-types'

const trustedCandidates = (artworkUrls, imageUrl) =>
  [...(artworkUrls || []), imageUrl]
    .filter(
      (value) => typeof value === 'string' && value.startsWith('https://'),
    )
    .filter((value, index, values) => values.indexOf(value) === index)

const ExternalArtwork = ({ artworkUrls, imageUrl, alt, className }) => {
  const candidates = useMemo(
    () => trustedCandidates(artworkUrls, imageUrl),
    [artworkUrls, imageUrl],
  )
  const [index, setIndex] = useState(0)
  const candidateKey = candidates.join('\u0000')

  useEffect(() => setIndex(0), [candidateKey])

  if (index >= candidates.length) {
    return (
      <div className={className} role="img" aria-label={alt || 'No artwork'}>
        {alt?.trim()?.slice(0, 1)?.toUpperCase() || '\u266a'}
      </div>
    )
  }

  return (
    <img
      className={className}
      src={candidates[index]}
      alt={alt || ''}
      loading="lazy"
      decoding="async"
      onError={() => setIndex((value) => value + 1)}
    />
  )
}

ExternalArtwork.propTypes = {
  artworkUrls: PropTypes.arrayOf(PropTypes.string),
  imageUrl: PropTypes.string,
  alt: PropTypes.string,
  className: PropTypes.string,
}

export default ExternalArtwork
