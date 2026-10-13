import { useEffect, useRef, useState, type FormEvent } from 'react'

type Track = { name: string; title: string; artist: string; album: string; duration: number; size: number; hasCover: boolean }
type FolderListing = { path: string; parent: string; folders: { name: string; path: string }[] }
type SortKey = 'title' | 'artist' | 'duration'

const songCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

function title(name: string) { return name.replace(/\.[^.]+$/, '').replace(/[_-]+/g, ' ') }
function time(value: number) {
  if (!Number.isFinite(value)) return '0:00'
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, '0')}`
}

function CoverArt({ track, folder, large = false }: { track?: Track; folder: string; large?: boolean }) {
  const source = track?.hasCover ? `/api/v1/music/cover?path=${encodeURIComponent(folder)}&name=${encodeURIComponent(track.name)}` : ''
  return <span className="music-art" aria-hidden="true">♫{source && <img key={source} src={source} alt="" loading={large ? 'eager' : 'lazy'} onError={(event) => { event.currentTarget.hidden = true }} />}</span>
}

export default function Music() {
  const [path, setPath] = useState(() => localStorage.getItem('homeos.music.path') ?? '')
  const [folder, setFolder] = useState('')
  const [tracks, setTracks] = useState<Track[]>([])
  const [query, setQuery] = useState('')
  const [sortBy, setSortBy] = useState<SortKey | null>(null)
  const [sortDescending, setSortDescending] = useState(false)
  const [showFolderPicker, setShowFolderPicker] = useState(() => !localStorage.getItem('homeos.music.path'))
  const [index, setIndex] = useState(-1)
  const [playing, setPlaying] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(0.8)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [browserOpen, setBrowserOpen] = useState(false)
  const [browserLoading, setBrowserLoading] = useState(false)
  const [browserError, setBrowserError] = useState('')
  const [browserListing, setBrowserListing] = useState<FolderListing | null>(null)
  const audio = useRef<HTMLAudioElement>(null)
  const lastVolume = useRef(0.8)
  const loadRequest = useRef(0)
  const active = tracks[index]
  const search = query.trim().toLocaleLowerCase()
  const visibleTracks = tracks.map((track, position) => ({ track, position })).filter(({ track }) =>
    [track.title, track.artist, track.album].some((value) => value.toLocaleLowerCase().includes(search)))
  if (sortBy) visibleTracks.sort((left, right) => {
    const first = left.track[sortBy]
    const second = right.track[sortBy]
    if (!first && second) return 1
    if (first && !second) return -1
    const comparison = sortBy === 'duration' ? Number(first) - Number(second) : songCollator.compare(String(first), String(second))
    return (sortDescending ? -comparison : comparison) || left.position - right.position
  })

  useEffect(() => { if (audio.current) audio.current.volume = volume }, [])
  useEffect(() => {
    const saved = localStorage.getItem('homeos.music.path')
    if (saved) void loadFolder(saved)
  }, [])

  function changeVolume(next: number) {
    setVolume(next)
    if (next > 0) lastVolume.current = next
    if (audio.current) audio.current.volume = next
  }

  async function loadFolder(next: string) {
    if (!next) return
    const request = ++loadRequest.current
    setLoading(true)
    setError('')
    try {
      const response = await fetch(`/api/v1/music?path=${encodeURIComponent(next)}`)
      if (!response.ok) {
        const body = await response.json().catch(() => null)
        throw new Error(body?.error?.message ?? `Could not open folder (${response.status})`)
      }
      const data = await response.json() as { tracks: Track[] }
      if (request !== loadRequest.current) return
      audio.current?.pause()
      setPlaying(false)
      setIndex(-1)
      setCurrentTime(0)
      setDuration(0)
      setTracks(data.tracks)
      setFolder(next)
      setPath(next)
      setBrowserOpen(false)
      setShowFolderPicker(false)
      localStorage.setItem('homeos.music.path', next)
    } catch (cause) { if (request === loadRequest.current) { setError(cause instanceof Error ? cause.message : 'Could not open folder'); setShowFolderPicker(true) } }
    finally { if (request === loadRequest.current) setLoading(false) }
  }

  async function browse(next: string) {
    setBrowserOpen(true)
    setBrowserLoading(true)
    setBrowserError('')
    try {
      const response = await fetch(`/api/v1/music/folders?path=${encodeURIComponent(next)}`)
      if (!response.ok) {
        const body = await response.json().catch(() => null)
        throw new Error(body?.error?.message ?? `Could not browse folder (${response.status})`)
      }
      setBrowserListing(await response.json() as FolderListing)
    } catch (cause) { setBrowserError(cause instanceof Error ? cause.message : 'Could not browse folder') }
    finally { setBrowserLoading(false) }
  }

  function load(event: FormEvent) {
    event.preventDefault()
    void loadFolder(path.trim())
  }

  function changeSort(next: SortKey) {
    if (sortBy === next) setSortDescending((current) => !current)
    else { setSortBy(next); setSortDescending(false) }
  }

  function playTrack(next: number) {
    if (next < 0 || next >= tracks.length) return
    if (next === index) {
      if (audio.current?.paused) void audio.current.play().catch(() => setError('This audio format could not be played.'))
      return
    }
    setError('')
    setIndex(next)
    setCurrentTime(0)
    setDuration(0)
  }

  useEffect(() => {
    if (index < 0) return
    void audio.current?.play().catch(() => setError('This audio format could not be played.'))
  }, [index, folder])

  function skip(direction: number) {
    if (!visibleTracks.length) return
    const current = visibleTracks.findIndex((item) => item.position === index)
    const next = current < 0 ? 0 : (current + direction + visibleTracks.length) % visibleTracks.length
    playTrack(visibleTracks[next].position)
  }

  return <section className="music" aria-label="HomeMusic player">
    <div className="music-library">
      {showFolderPicker && <form className="music-folder-form" onSubmit={(event) => void load(event)}>
        <label htmlFor="music-folder">Server music folder</label>
        <div><input id="music-folder" value={path} onChange={(event) => setPath(event.target.value)} placeholder="C:\Music or /media/music" spellCheck={false} /><button type="button" className="secondary-button" onClick={() => void browse(path.trim())}>Browse folders</button><button type="submit" disabled={loading || !path.trim()}>{loading ? 'Loading…' : 'Load music'}</button></div>
        <p>Browse folders on the HomeOS server, or enter an absolute path and load it.</p>
      </form>}
      {browserOpen && <div className="music-browser-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setBrowserOpen(false) }}>
        <div className="music-browser" role="dialog" aria-modal="true" aria-labelledby="music-browser-title">
          <div className="music-browser-header"><div><p className="eyebrow">HomeOS server</p><h2 id="music-browser-title">Choose a music folder</h2></div><button type="button" className="secondary-button" onClick={() => setBrowserOpen(false)} aria-label="Close folder browser">✕</button></div>
          <p className="music-browser-path">{browserListing?.path || 'Drives and roots'}</p>
          {browserError && <p className="music-error" role="alert">{browserError}</p>}
          {browserLoading ? <p className="music-empty">Loading folders…</p> : <div className="music-browser-list">
            {browserListing?.path && <button type="button" onClick={() => void browse(browserListing.parent)}>↖ &nbsp; Up one level</button>}
            {browserError && <button type="button" onClick={() => void browse('')}>Browse drives and roots</button>}
            {browserListing?.folders.map((item) => <button type="button" key={item.path} onClick={() => void browse(item.path)}><span aria-hidden="true">▣</span> {item.name}</button>)}
            {browserListing && browserListing.folders.length === 0 && <p className="music-empty">No subfolders here.</p>}
          </div>}
          <div className="music-browser-actions"><button type="button" className="secondary-button" onClick={() => setBrowserOpen(false)}>Cancel</button><button type="button" disabled={!browserListing?.path || browserLoading || loading} onClick={() => void loadFolder(browserListing!.path)}>{loading ? 'Loading…' : 'Use this folder'}</button></div>
        </div>
      </div>}
      {error && <p className="music-error" role="alert">{error}</p>}
      {folder && <div className="music-search"><span aria-hidden="true">⌕</span><input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search title, artist, or album" aria-label="Search songs by title, artist, or album" /></div>}
      <div className={`music-heading ${showFolderPicker ? '' : 'is-compact'}`}><div><p className="eyebrow">Your library</p><h2>{folder ? 'Music' : loading ? 'Loading music…' : 'Choose a folder'}</h2></div><div className="music-heading-actions"><span>{tracks.length} {tracks.length === 1 ? 'track' : 'tracks'}</span>{folder && <button type="button" className="secondary-button" onClick={() => setShowFolderPicker((current) => !current)}>{showFolderPicker ? 'Hide folder controls' : 'Change folder'}</button>}</div></div>
      {folder && <p className="music-path" title={folder}>{folder}</p>}
      <div className="music-table-head"><span>#</span><button type="button" className="music-sort-button" onClick={() => changeSort('title')} aria-label={`Sort by title${sortBy === 'title' ? `, ${sortDescending ? 'descending' : 'ascending'}` : ''}`}>Title {sortBy === 'title' && (sortDescending ? '↓' : '↑')}</button><button type="button" className="music-sort-button" onClick={() => changeSort('artist')} aria-label={`Sort by artist${sortBy === 'artist' ? `, ${sortDescending ? 'descending' : 'ascending'}` : ''}`}>Artist {sortBy === 'artist' && (sortDescending ? '↓' : '↑')}</button><span>Album</span><button type="button" className="music-sort-button music-sort-duration" onClick={() => changeSort('duration')} aria-label={`Sort by duration${sortBy === 'duration' ? `, ${sortDescending ? 'descending' : 'ascending'}` : ''}`}>◷ {sortBy === 'duration' && (sortDescending ? '↓' : '↑')}</button></div>
      <ol className="music-list">
        {visibleTracks.map(({ track, position }, rowNumber) => <li key={track.name}>
          <button className={`music-row ${index === position ? 'is-current' : ''}`} type="button" onClick={() => playTrack(position)} aria-label={`Play ${track.title || title(track.name)}`}>
            <span className="music-number">{index === position && playing ? <span className="music-equalizer" aria-hidden="true"><i /><i /><i /></span> : rowNumber + 1}</span>
            <span className="music-track"><CoverArt track={track} folder={folder} /><span><strong>{track.title || title(track.name)}</strong><small>{track.artist || 'Unknown artist'} · {track.album || 'Unknown album'}</small></span></span>
            <span className="music-artist">{track.artist || 'Unknown artist'}</span>
            <span className="music-album">{track.album || 'Unknown album'}</span>
            <span className="music-duration">{track.duration > 0 ? time(track.duration) : '—'}</span>
          </button>
        </li>)}
      </ol>
      {folder && tracks.length === 0 && <p className="music-empty">No supported audio files in this folder.</p>}
      {folder && tracks.length > 0 && visibleTracks.length === 0 && <p className="music-empty">No songs match “{query}”.</p>}
      {!folder && !loading && <p className="music-empty">Enter a folder path above to start listening.</p>}
    </div>
    <div className="music-player">
      <div className="music-now"><CoverArt track={active} folder={folder} large /><span><strong>{active ? active.title || title(active.name) : 'Nothing playing'}</strong><small>{active ? active.artist || 'Unknown artist' : 'Select a track'}</small></span></div>
      <div className="music-playback">
        <div className="music-controls"><button type="button" aria-label="Previous track" onClick={() => skip(-1)} disabled={!visibleTracks.length}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 5v14M19 5 7 12l12 7z" /></svg></button><button type="button" className="music-play-button" aria-label={playing ? 'Pause' : 'Play'} onClick={() => { if (index < 0) playTrack(visibleTracks[0].position); else if (playing) audio.current?.pause(); else void audio.current?.play().catch(() => setError('This audio format could not be played.')) }} disabled={index < 0 && !visibleTracks.length}>{playing ? <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 5v14M17 5v14" /></svg> : <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m8 5 11 7-11 7z" /></svg>}</button><button type="button" aria-label="Next track" onClick={() => skip(1)} disabled={!visibleTracks.length}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M19 5v14M5 5l12 7-12 7z" /></svg></button></div>
        <div className="music-timeline"><span>{time(currentTime)}</span><input type="range" aria-label="Seek" min="0" max={duration || 1} step="0.1" value={Math.min(currentTime, duration || 1)} disabled={!active || !duration} onChange={(event) => { if (audio.current) audio.current.currentTime = Number(event.target.value); setCurrentTime(Number(event.target.value)) }} style={{ '--progress': `${duration ? currentTime / duration * 100 : 0}%` } as React.CSSProperties} /><span>{time(duration)}</span></div>
      </div>
      <div className="music-volume"><button type="button" aria-label={volume === 0 ? 'Unmute' : 'Mute'} title={volume === 0 ? 'Unmute' : 'Mute'} onClick={() => changeVolume(volume === 0 ? lastVolume.current : 0)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 9v6h4l5 4V5L7 9z" />{volume === 0 ? <path d="m16 9 5 6m0-6-5 6" /> : <path d="M16 8c2 1 3 2 3 4s-1 3-3 4" />}</svg></button><input type="range" aria-label="Volume" min="0" max="1" step="0.01" value={volume} onChange={(event) => changeVolume(Number(event.target.value))} style={{ '--volume': `${volume * 100}%` } as React.CSSProperties} /></div>
    </div>
    <audio ref={audio} src={active ? `/api/v1/music/audio?path=${encodeURIComponent(folder)}&name=${encodeURIComponent(active.name)}` : undefined} preload="metadata" onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)} onDurationChange={(event) => setDuration(event.currentTarget.duration)} onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)} onEnded={() => skip(1)} onError={() => { if (active) setError('This track could not be loaded or played.') }} />
  </section>
}
