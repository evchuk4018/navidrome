export const getQuickPickItemKey = (item) => {
  if (!item) return ''
  return String(
    item.itemKey ||
      item.key ||
      item.song?.id ||
      item.playlist?.id ||
      item.id ||
      '',
  )
}

export const getQuickPickSection = (item) =>
  item?.section === 'start_radio' || item?.kind === 'recommendation'
    ? 'start_radio'
    : 'listen_again'

const addUntil = (target, candidates, used, limit) => {
  for (const item of candidates) {
    if (target.length >= limit) break
    const key = getQuickPickItemKey(item)
    if (!key || used.has(key)) continue
    target.push(item)
    used.add(key)
  }
}

// Keep both shelves predictable while allowing a sparse section to borrow from
// the other section. The API remains authoritative for order; this helper only
// caps and backfills the flattened response for the six-tile layout.
export const splitQuickPickShelves = (items = [], limit = 6) => {
  const listenAgainCandidates = items.filter(
    (item) => getQuickPickSection(item) === 'listen_again',
  )
  const startRadioCandidates = items.filter(
    (item) => getQuickPickSection(item) === 'start_radio',
  )
  const listenAgain = []
  const startRadio = []
  const used = new Set()

  addUntil(listenAgain, listenAgainCandidates, used, limit)
  addUntil(startRadio, startRadioCandidates, used, limit)
  addUntil(listenAgain, startRadioCandidates, used, limit)
  addUntil(startRadio, listenAgainCandidates, used, limit)

  return { listenAgain, startRadio }
}

// Small request guard shared by the Quick Pick UI and its tests. Starting a
// different tile aborts the previous request; clicking the same tile again is
// ignored until its request settles.
export const beginQuickPickRequest = (ref, key) => {
  if (!key || ref.current?.key === key) return null
  ref.current?.controller?.abort()
  const token = (ref.current?.token || 0) + 1
  const controller = new AbortController()
  ref.current = { token, key, controller }
  return { token, controller }
}

export const isCurrentQuickPickRequest = (ref, token) =>
  ref.current?.token === token
