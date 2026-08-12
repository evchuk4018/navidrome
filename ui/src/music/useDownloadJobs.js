import { useCallback, useEffect, useState } from 'react'
import * as musicProvider from './provider'

export const useDownloadJobs = () => {
  const [jobs, setJobs] = useState([])

  const refreshJobs = useCallback(() => {
    return musicProvider
      .listDownloads()
      .then((nextJobs) => setJobs(Array.isArray(nextJobs) ? nextJobs : []))
      .catch(() => undefined)
  }, [])

  useEffect(() => {
    refreshJobs()
    const timer = window.setInterval(refreshJobs, 4000)
    return () => window.clearInterval(timer)
  }, [refreshJobs])

  return { jobs, refreshJobs }
}
