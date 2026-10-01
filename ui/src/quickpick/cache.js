const sections = new Map()

// Clearing the cache advances the generation used by in-flight requests. A
// request from a previous authentication session can therefore never write
// its result into a later session's cache entry.
let generation = 0

const keyFor = (userId, section) => `${userId || ''}:${section}`

const clone = (value) => {
  if (value == null || typeof value !== 'object') return value
  if (typeof structuredClone === 'function') return structuredClone(value)
  return JSON.parse(JSON.stringify(value))
}

export const getQuickPickCacheEntry = (userId, section) => {
  const entry = sections.get(keyFor(userId, section))
  if (!entry) return null
  if (entry.status === 'success') {
    return { ...entry, value: clone(entry.value) }
  }
  return entry
}

export const getQuickPickCacheGeneration = () => generation

export const ensureQuickPickImpressions = (
  userId,
  viewId,
  itemKeys,
  createRequest,
) => {
  const key = keyFor(userId, 'discovery')
  const entry = sections.get(key)
  if (!entry || entry.status !== 'success' || !itemKeys.length) return null
  if (entry.impression?.viewId === viewId) return entry.impression.request

  const request = createRequest()
  sections.set(key, {
    ...entry,
    impression: { viewId, request },
  })
  return request
}

export const loadQuickPickSection = (
  userId,
  section,
  loader,
  { retry = false } = {},
) => {
  const key = keyFor(userId, section)
  const existing = sections.get(key)

  if (!retry && existing) {
    if (existing.status === 'success') {
      return Promise.resolve().then(() => clone(existing.value))
    }
    if (existing.status === 'pending') return existing.promise
    return Promise.reject(existing.error)
  }

  const requestGeneration = generation
  const pending = { status: 'pending', generation: requestGeneration }
  const promise = Promise.resolve()
    .then(loader)
    .then(
      (value) => {
        let snapshot
        try {
          snapshot = clone(value)
        } catch (error) {
          const current = sections.get(key)
          if (generation === requestGeneration && current === pending) {
            sections.set(key, { status: 'error', error })
          }
          throw error
        }
        const current = sections.get(key)
        if (generation === requestGeneration && current === pending) {
          sections.set(key, {
            status: 'success',
            value: snapshot,
          })
        }
        return snapshot
      },
      (error) => {
        const current = sections.get(key)
        if (generation === requestGeneration && current === pending) {
          sections.set(key, { status: 'error', error })
        }
        throw error
      },
    )

  pending.promise = promise
  sections.set(key, pending)
  return promise
}

export const clearQuickPickCache = () => {
  generation += 1
  sections.clear()
}
