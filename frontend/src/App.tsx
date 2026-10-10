import { useCallback, useEffect, useState, type FormEvent } from 'react'

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
	const [session, setSession] = useState<Session | null>(null)
	const [checkingSession, setCheckingSession] = useState(true)
	const [username, setUsername] = useState('')
	const [password, setPassword] = useState('')
	const [loginError, setLoginError] = useState<string | null>(null)
	const [signingIn, setSigningIn] = useState(false)
  const [data, setData] = useState<Status | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const response = await fetch('/api/v1/status', { cache: 'no-store' })
	  if (response.status === 401) {
	    setSession(null)
	    setData(null)
	    return
	  }
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
    const timer = window.setInterval(() => void refresh(), 15_000)
    return () => window.clearInterval(timer)
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
	      <p className="login-intro">Use your HomeOS administrator account.</p>
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

  return (
    <main className="shell">
      <header className="header">
        <div>
          <p className="eyebrow">Personal cloud · Homelab</p>
          <h1>HomeOS</h1>
          <p className="subtitle">A clear view of your home server.</p>
        </div>
		<div className="header-actions"><span className="version">{session.user.username}</span><button type="button" className="secondary-button" onClick={() => void signOut()}>Sign out</button></div>
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
