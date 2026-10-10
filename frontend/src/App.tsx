import { useCallback, useEffect, useState } from 'react'

type Metric = {
  id: string
  label: string
  value: number
  unit: string
  scope: 'application' | 'container' | 'host'
  state: 'available' | 'stale' | 'unavailable'
  sampledAt: string
}

type Status = {
  status: string
  version: string
  sampledAt: string
  metrics: Metric[]
}

function formatMetric(metric: Metric): string {
  if (metric.state !== 'available') return 'Unavailable'
  if (metric.unit === 'bytes') return `${(metric.value / 1024 / 1024).toFixed(1)} MiB`
  if (metric.unit === 'seconds') {
    const seconds = Math.floor(metric.value)
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m ${seconds % 60}s`
  }
  return `${metric.value} ${metric.unit}`
}

export default function App() {
  const [data, setData] = useState<Status | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const response = await fetch('/api/v1/status', { cache: 'no-store' })
      if (!response.ok) throw new Error(`Status request failed (${response.status})`)
      const next: Status = await response.json()
      setData(next)
      setError(null)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load status')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => void refresh(), 15_000)
    return () => window.clearInterval(timer)
  }, [refresh])

  return (
    <main className="shell">
      <header className="header">
        <div>
          <p className="eyebrow">Personal cloud · Homelab</p>
          <h1>HomeOS</h1>
          <p className="subtitle">A clear view of your home server.</p>
        </div>
        <span className="version">{data?.version ?? 'v0.1 development'}</span>
      </header>

      <section className="overview" aria-live="polite">
        <div>
          <p className="eyebrow">System overview</p>
          <h2>{loading ? 'Connecting…' : error ? 'Connection needs attention' : 'Application is healthy'}</h2>
          <p>{error ?? 'Live measurements from the HomeOS application.'}</p>
        </div>
        <div className={`status-pill ${error ? 'status-error' : ''}`}>
          <span className="status-dot" /> {error ? 'Unavailable' : loading ? 'Checking' : 'Online'}
        </div>
      </section>

      <div className="section-heading">
        <div>
          <p className="eyebrow">Live status</p>
          <h2>Application metrics</h2>
        </div>
        <button type="button" onClick={() => void refresh()}>Refresh now</button>
      </div>

      <section className="cards" aria-label="Application metrics">
        {data?.metrics.map((metric) => (
          <article className="card" key={metric.id}>
            <div className="card-top"><span>{metric.label}</span><span className="scope">{metric.scope}</span></div>
            <p className="value">{formatMetric(metric)}</p>
            <p className="card-foot">Updated {new Date(metric.sampledAt).toLocaleTimeString()}</p>
          </article>
        ))}
        {!data && <p className="empty">{loading ? 'Loading measurements…' : 'No measurements available.'}</p>}
      </section>

      <footer>
        <span>Automatic refresh every 15 seconds</span>
        <span>{data ? `Last successful update: ${new Date(data.sampledAt).toLocaleTimeString()}` : 'Awaiting first update'}</span>
      </footer>
    </main>
  )
}
