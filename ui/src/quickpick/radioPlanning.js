export const isRadioPlanning = (status) =>
  status === 'selecting' ||
  status === 'downloading' ||
  status === 'waiting_for_scan' ||
  status === 'retrying'

export const radioPlanningMessage = (status) => {
  if (status === 'exhausted' || status === 'no_discovery') {
    return 'No more eligible tracks are available in this session.'
  }
  if (status === 'error') {
    return 'Unable to extend radio right now — try again shortly.'
  }
  if (status === 'waiting_for_scan') {
    return 'Almost ready — adding your new song…'
  }
  if (status === 'retrying') {
    return 'That match was unavailable — trying another…'
  }
  return 'Pondering next song…'
}
