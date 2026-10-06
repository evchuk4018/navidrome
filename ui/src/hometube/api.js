import config from '../config'

/**
 * Return whether the HomeTube integration has been configured. HomeTube is
 * deliberately opt-in: an empty mount point leaves the music application
 * entirely untouched.
 */
export const isHomeTubeConfigured = () =>
  typeof config.homeTubeBaseURL === 'string' &&
  config.homeTubeBaseURL.trim().length > 0

const splitPathAndSuffix = (value) => {
  const source = String(value || '')
  const match = source.match(/^([^?#]*)([?#].*)?$/)
  return {
    pathname: match ? match[1] : source,
    suffix: match?.[2] || '',
  }
}

/**
 * Build a HomeTube API URL without using Navidrome's authenticated data
 * provider. The HomeTube reverse proxy is mounted at the configured base and
 * its Next route handlers intentionally use a trailing slash.
 *
 * Examples:
 *   /hometube + /feed?limit=40 -> /hometube/api/feed/?limit=40
 *   /hometube/ + /channels/   -> /hometube/api/channels/
 */
export const homeTubeApiPath = (path = '') => {
  if (!isHomeTubeConfigured()) return ''

  const base = config.homeTubeBaseURL.trim().replace(/\/+$/, '')
  const { pathname: sourcePathname, suffix } = splitPathAndSuffix(path)
  let pathname = sourcePathname.replace(/^\/+/, '')
  if (pathname.toLowerCase().startsWith('api/')) pathname = pathname.slice(4)
  if (pathname.toLowerCase() === 'api') pathname = ''
  pathname = pathname.replace(/^\/+|\/+$/g, '')

  const apiBase = /\/api$/i.test(base) ? base : `${base}/api`
  return `${apiBase}/${pathname ? `${pathname}/` : ''}${suffix}`
}

const responseMessage = (payload, response) => {
  if (payload && typeof payload === 'object') {
    if (typeof payload.error === 'string' && payload.error.trim()) {
      return payload.error.trim()
    }
    if (typeof payload.message === 'string' && payload.message.trim()) {
      return payload.message.trim()
    }
  }
  if (typeof payload === 'string' && payload.trim()) return payload.trim()
  return response.statusText || 'The HomeTube service did not provide a reason.'
}

/**
 * Make a HomeTube request and parse its JSON response. No Navidrome JWT or
 * data-provider headers are added; callers can supply only the headers that
 * the HomeTube endpoint needs (usually content-type for JSON requests).
 */
export const homeTubeRequest = async (path, options = {}) => {
  const url = homeTubeApiPath(path)
  if (!url) {
    throw new Error('HomeTube is not configured.')
  }

  let response
  try {
    response = await fetch(url, options)
  } catch (error) {
    const message = error instanceof Error ? error.message : 'network error'
    throw new Error(`Unable to reach HomeTube: ${message}`)
  }

  const contentType = response.headers.get('content-type') || ''
  let payload = null
  if (response.status !== 204) {
    try {
      payload = contentType.includes('json')
        ? await response.json()
        : await response.text()
    } catch {
      payload = null
    }
  }

  if (!response.ok) {
    throw new Error(
      `HomeTube request failed (${response.status}): ${responseMessage(
        payload,
        response,
      )}`,
    )
  }
  return payload
}

const jsonOptions = (method, body) => ({
  method,
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify(body),
})

export const getHomeTubeFeed = (limit = 40) =>
  homeTubeRequest(`/feed/?limit=${encodeURIComponent(limit)}`, {
    cache: 'no-store',
  })

export const reportHomeTubeImpressions = (videoIds) =>
  homeTubeRequest('/feed/', jsonOptions('POST', { videoIds }))

export const refreshHomeTubeFeed = (videoIds, limit = 40) =>
  homeTubeRequest(
    `/feed/refresh/?limit=${encodeURIComponent(limit)}`,
    jsonOptions('POST', { videoIds }),
  )

export const listHomeTubeChannels = () =>
  homeTubeRequest('/channels/', { cache: 'no-store' })

export const addHomeTubeChannel = (url) =>
  homeTubeRequest('/channels/', jsonOptions('POST', { url }))

export const getHomeTubeChannel = (channelId, limit = 50, offset = 0) =>
  homeTubeRequest(
    `/channels/${encodeURIComponent(
      channelId,
    )}/?limit=${encodeURIComponent(limit)}&offset=${encodeURIComponent(offset)}`,
    { cache: 'no-store' },
  )

export const updateHomeTubeSubscription = (channelId, subscribed) =>
  homeTubeRequest(
    `/channels/${encodeURIComponent(channelId)}/subscription/`,
    jsonOptions('PUT', { subscribed }),
  )

export const refreshHomeTubeChannel = (channelId) =>
  homeTubeRequest(`/channels/${encodeURIComponent(channelId)}/refresh/`, {
    method: 'POST',
  })

export const requestHomeTubeDownload = (videoId) =>
  homeTubeRequest(`/videos/${encodeURIComponent(videoId)}/download/`, {
    method: 'POST',
  })

export const getHomeTubeJob = (jobId) =>
  homeTubeRequest(`/jobs/${encodeURIComponent(jobId)}/`, {
    cache: 'no-store',
  })
