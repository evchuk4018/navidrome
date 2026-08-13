export const isRadioPlanning = (status) =>
  status === 'selecting' ||
  status === 'downloading' ||
  status === 'waiting_for_scan'

export const radioPlanningMessage = (status) => {
  if (status === 'no_discovery') {
    return "Couldn't find a new match — continuing with similar music."
  }
  if (status === 'waiting_for_scan') {
    return 'Almost ready — adding your new song…'
  }
  return 'Pondering next song…'
}
