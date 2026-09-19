export const radioReadyAhead = (queue = [], currentIndex = 0, sessionId) =>
  queue
    .slice(Math.max(0, currentIndex) + 1)
    .filter(
      (item) =>
        item.radioSessionId === sessionId &&
        item.radioItemId &&
        !item.radioPending,
    ).length

export const nextPlayableRadioIndex = (
  audioLists = [],
  currentIndex = -1,
  sessionId,
) =>
  audioLists.findIndex(
    (item, index) =>
      index > currentIndex &&
      (!sessionId || item.radioSessionId === sessionId) &&
      item.radioItemId &&
      !item.radioPending &&
      !!item.musicSrc,
  )

export const shouldRetryRadioStream = (attempts, key) => {
  if (!key || attempts.has(key)) return false
  attempts.add(key)
  return true
}
