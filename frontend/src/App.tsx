import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import Drive from './Drive'
import Music from './Music'

type Session = {
  user: { id: number; username: string; displayName: string; role: string }
  csrfToken: string
}

type Metric = {
  id: string
  label: string
  value: number
  unit: string
  scope: 'application' | 'container' | 'host'
  state: 'available' | 'stale' | 'unavailable'
  sampledAt: string
  detail?: string
  reason?: string
}

type Status = {
  status: string
  version: string
  sampledAt: string
  metrics: Metric[]
}

const services = [
  { id: 'overview', label: 'Overview', group: 'Home', description: 'Your HomeOS starting point.', ready: true },
  { id: 'drive', label: 'HomeDrive', group: 'Personal cloud', description: 'Files and folders on your homelab disk.', ready: true },
  { id: 'music', label: 'HomeMusic', group: 'Personal cloud', description: 'Play music from a folder on your HomeOS server.', ready: true },
  { id: 'photos', label: 'HomePhotos', group: 'Personal cloud', description: 'Photo galleries and albums will be added later.', ready: false },
  { id: 'git', label: 'HomeGit', group: 'Personal cloud', description: 'Repository hosting and browsing will be added later.', ready: false },
  { id: 'sync', label: 'HomeSync', group: 'Personal cloud', description: 'Cross-device synchronization will be added later.', ready: false },
  { id: 'monitor', label: 'HomeMonitor', group: 'Infrastructure', description: 'Live host and application measurements.', ready: true },
  { id: 'docker', label: 'HomeDocker', group: 'Infrastructure', description: 'Safe container monitoring and controls will be added later.', ready: false },
  { id: 'network', label: 'HomeNetwork', group: 'Infrastructure', description: 'Network information and administration will be added later.', ready: false },
  { id: 'backup', label: 'HomeBackup', group: 'Infrastructure', description: 'Scheduled backup and restore controls will be added later.', ready: false },
  { id: 'cam', label: 'HomeCam', group: 'Infrastructure', description: 'Camera viewing and recording will be added later.', ready: false },
  { id: 'tasks', label: 'HomeTasks', group: 'Automation', description: 'Scheduled jobs and workflows will be added later.', ready: false },
  { id: 'ai', label: 'HomeAI', group: 'Automation', description: 'Optional AI-assisted features will be added later.', ready: false },
] as const

function sectionFromHash(): string {
  const id = window.location.hash.replace(/^#\//, '')
  return services.some((service) => service.id === id) ? id : 'overview'
}

function formatMetric(metric: Metric): string {
  if (metric.state === 'unavailable') return 'Unavailable'
  if (metric.unit === 'percent') return `${metric.value.toFixed(1)}%`
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
	const [session, setSession] = useState<Session | null>(null)
	const [checkingSession, setCheckingSession] = useState(true)
	const [username, setUsername] = useState('')
	const [password, setPassword] = useState('')
	const [loginError, setLoginError] = useState<string | null>(null)
	const [signingIn, setSigningIn] = useState(false)
  const [data, setData] = useState<Status | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [activeSection, setActiveSection] = useState(sectionFromHash)
  const [navOpen, setNavOpen] = useState(false)
  const activeRequest = useRef<AbortController | null>(null)

  useEffect(() => {
    const onHashChange = () => {
      setActiveSection(sectionFromHash())
      setNavOpen(false)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const refresh = useCallback(async () => {
    if (activeRequest.current) return
    const controller = new AbortController()
    activeRequest.current = controller
    let timedOut = false
    const timeout = window.setTimeout(() => {
      timedOut = true
      controller.abort()
    }, 5_000)
    try {
      const response = await fetch('/api/v1/status', { cache: 'no-store', signal: controller.signal })
	  if (response.status === 401) {
	    setSession(null)
	    setData(null)
	    setError(null)
	    return
	  }
      if (!response.ok) throw new Error(`Status request failed (${response.status})`)
      const next: Status = await response.json()
      setData(next)
      setError(null)
    } catch (cause) {
      if (!controller.signal.aborted || timedOut) {
        setError(timedOut ? 'Status request timed out' : cause instanceof Error ? cause.message : 'Unable to load status')
      }
    } finally {
      window.clearTimeout(timeout)
      if (activeRequest.current === controller) activeRequest.current = null
      setLoading(false)
    }
  }, [])

  useEffect(() => {
	void fetch('/api/v1/auth/me', { cache: 'no-store' })
	  .then(async (response) => {
	    if (response.ok) setSession((await response.json()) as Session)
	  })
	  .catch(() => setLoginError('Cannot reach HomeOS. Check that the server is running.'))
	  .finally(() => setCheckingSession(false))
	}, [])

  useEffect(() => {
	if (!session) return
    void refresh()
    const timer = window.setInterval(() => void refresh(), 1_000)
    return () => {
      window.clearInterval(timer)
      activeRequest.current?.abort()
      activeRequest.current = null
    }
  }, [refresh, session])

	async function signIn(event: FormEvent<HTMLFormElement>) {
	  event.preventDefault()
	  setSigningIn(true)
	  setLoginError(null)
	  try {
	    const response = await fetch('/api/v1/auth/login', {
	      method: 'POST',
	      headers: { 'Content-Type': 'application/json' },
	      body: JSON.stringify({ username, password }),
	    })
	    if (!response.ok) throw new Error(response.status === 429 ? 'Too many attempts. Wait a minute and try again.' : 'Invalid username or password.')
	    setSession((await response.json()) as Session)
	    setData(null)
	    setError(null)
	    setLoading(true)
	    setPassword('')
	  } catch (cause) {
	    setLoginError(cause instanceof Error ? cause.message : 'Unable to sign in')
	  } finally {
	    setSigningIn(false)
	  }
	}

	async function signOut() {
	  if (!session) return
	  try {
	    const response = await fetch('/api/v1/auth/logout', {
	      method: 'POST',
	      headers: { 'X-CSRF-Token': session.csrfToken },
	    })
	    if (!response.ok) throw new Error('Unable to sign out. Please try again.')
	    setSession(null)
	    setData(null)
	    setError(null)
	  } catch {
	    setError('Unable to sign out. Please try again.')
	  }
	}

	if (checkingSession) return <main className="shell"><p>Checking session…</p></main>

	if (!session) return (
	  <main className="shell login-shell">
	    <p className="eyebrow">Personal cloud · Homelab</p>
	    <h1>HomeOS</h1>
	    <section className="login-card">
	      <h2>Sign in</h2>
	      <p className="login-intro">Use your HomeOS account.</p>
	      <form onSubmit={(event) => void signIn(event)}>
	        <label htmlFor="username">Username</label>
	        <input id="username" name="username" autoComplete="username" required value={username} onChange={(event) => setUsername(event.target.value)} />
	        <label htmlFor="password">Password</label>
	        <input id="password" name="password" type="password" autoComplete="current-password" required value={password} onChange={(event) => setPassword(event.target.value)} />
	        {loginError && <p className="form-error" role="alert">{loginError}</p>}
	        <button type="submit" disabled={signingIn}>{signingIn ? 'Signing in…' : 'Sign in'}</button>
	      </form>
	    </section>
	  </main>
	)

  const current = services.find((service) => service.id === activeSection) ?? services[0]
  const systemOnline = !error && data?.status === 'healthy'
  const systemLabel = loading && !data ? 'Checking system' : systemOnline ? 'System online' : 'Status unavailable'
  const summaryValue = (id: string) => {
    if (error) return 'Unavailable'
    if (!data) return loading ? 'Loading…' : 'Unavailable'
    const metric = data.metrics.find((item) => item.id === id)
    return metric?.state === 'available' ? formatMetric(metric) : 'Unavailable'
  }

  const health = <section className="overview" aria-label="System status">
    <div className="status-heading">
      <h2>System status</h2>
      <span role="status" className={`system-state ${systemOnline ? 'state-online' : error ? 'state-error' : ''}`}>
        <span className="status-dot" />{systemLabel}
      </span>
    </div>
    <dl className="status-metrics">
      <div><dt>CPU usage</dt><dd>{summaryValue('host_cpu')}</dd></div>
      <div><dt>RAM usage</dt><dd>{summaryValue('host_memory')}</dd></div>
      <div><dt>Storage usage</dt><dd>{summaryValue('host_data_disk')}</dd></div>
      <div><dt>Service health</dt><dd>{systemOnline ? 'Healthy' : loading && !data ? 'Checking…' : 'Unavailable'}</dd></div>
    </dl>
    {error && <p className="status-message" role="alert">{error}. Last successful sample: {data ? new Date(data.sampledAt).toLocaleTimeString() : 'none'}.</p>}
  </section>

  return <div className="app-layout">
    <aside className="sidebar">
      <div className="sidebar-top">
        <a className="sidebar-brand" href="#/overview" onClick={() => setNavOpen(false)}>HomeOS<span>Personal cloud · Homelab</span></a>
        <button className="mobile-menu-button secondary-button" type="button" aria-expanded={navOpen} aria-controls="service-navigation" onClick={() => setNavOpen((open) => !open)}>{navOpen ? 'Close menu' : 'Menu'}</button>
      </div>
      <nav className="sidebar-nav" id="service-navigation" data-open={navOpen} aria-label="Services">
        {['Home', 'Personal cloud', 'Infrastructure', 'Automation'].map((group) =>
          <div className="nav-group" key={group}>
            <p className="nav-group-title">{group}</p>
            {services.filter((service) => service.group === group).map((service) =>
              service.ready ? <a key={service.id} className={`nav-link ${activeSection === service.id ? 'nav-active' : ''}`} href={`#/${service.id}`} aria-current={activeSection === service.id ? 'page' : undefined}>{service.label}</a> :
                <span key={service.id} className="nav-link nav-unavailable" aria-disabled="true" title="Coming soon">{service.label}<span className="sr-only"> (Coming soon)</span></span>)}
          </div>)}
      </nav>
    </aside>

    <main className="main-panel">
      <header className="header">
        <div><p className="eyebrow">{current.group}</p><h1>{current.label}</h1><p className="subtitle">{current.description}</p></div>
        <div className="header-actions"><span className="version">{session.user.username}</span><button type="button" className="secondary-button" onClick={() => void signOut()}>Sign out</button></div>
      </header>

      {activeSection === 'overview' && <>
        {health}
        <div className="section-heading"><h2>Services</h2></div>
        <section className="service-grid" aria-label="Available services">
          <article className="service-card"><h3>HomeDrive</h3><p>File storage and folders</p><span className="service-state">Enabled</span><a href="#/drive">Open service</a></article>
          <article className="service-card"><h3>HomeMusic</h3><p>Music from a server folder</p><span className="service-state">Enabled</span><a href="#/music">Open service</a></article>
          <article className="service-card"><h3>HomeMonitor</h3><p>Host and application metrics</p><span className={`service-state ${error ? 'service-state-error' : ''}`}>{systemOnline ? 'Healthy' : loading && !data ? 'Checking' : 'Unavailable'}</span><a href="#/monitor">Open service</a></article>
        </section>
      </>}

      {activeSection === 'monitor' && <>
        {health}
        <div className="section-heading"><h2>System metrics</h2><button type="button" onClick={() => void refresh()}>Refresh now</button></div>
        <section className="cards" aria-label="System metrics">
          {data?.metrics.map((metric) => {
            const stale = Boolean(error) || metric.state === 'stale'
            return <article className={`card ${stale ? 'card-stale' : ''}`} key={metric.id}>
              <div className="card-top"><span>{metric.label}</span><span className="scope">{metric.scope}</span></div>
              {stale && <span className="stale-label">Last known value</span>}
              <p className="value">{formatMetric(metric)}</p>
              {(metric.detail || metric.reason) && <p className="metric-detail">{metric.detail ?? metric.reason}</p>}
              <p className="card-foot">{stale ? 'Last good sample' : 'Updated'} {new Date(metric.sampledAt).toLocaleTimeString()}</p>
            </article>
          })}
          {!data && <p className="empty">{loading ? 'Loading measurements…' : 'No measurements available.'}</p>}
        </section>
        <footer><span>Automatic refresh every second</span><span>{data ? `Last successful update: ${new Date(data.sampledAt).toLocaleTimeString()}` : 'Awaiting first update'}</span></footer>
      </>}

      {activeSection === 'drive' && <Drive csrfToken={session.csrfToken} />}
      {activeSection === 'music' && <Music />}

      {!current.ready && <section className="planned-panel"><span className="planned-badge">Coming soon</span><h2>{current.label} is not active yet</h2><p>{current.description} This page will become its own dashboard as the module is built. No controls are available here yet.</p></section>}
    </main>
  </div>
}
