import { useEffect, useRef, useState, type FormEvent } from 'react'

type Track = { id?: string; name: string; path?: string; title: string; artist: string; album: string; duration: number; size: number; hasCover: boolean; available?: boolean; loudnessLufs?: number; loudnessPeakDbfs?: number }
type FolderListing = { path: string; parent: string; folders: { name: string; path: string }[] }
type SortKey = 'title' | 'artist' | 'duration'
type Playlist = { id: string; name: string; trackIds: string[] }
type Personal = { favorites: string[]; recent: Track[]; playCounts: Record<string, number>; playlists: Playlist[]; queue: string[]; player: { trackId: string; position: number; shuffle: boolean; repeat: 'off' | 'all' | 'one' } }
type MusicCache = { root: string; tracks: Track[]; hasMore: boolean; scanError: string }

function musicCacheKey(userId: number) { return `homeos.music.catalog.v1.${userId}` }
function readMusicCache(userId: number): MusicCache | null {
  try {
    const value = sessionStorage.getItem(musicCacheKey(userId))
    if (!value) return null
    const cache = JSON.parse(value) as MusicCache
    return typeof cache.root === 'string' && Array.isArray(cache.tracks) ? cache : null
  } catch { return null }
}
function writeMusicCache(userId: number, cache: MusicCache) {
  try { sessionStorage.setItem(musicCacheKey(userId), JSON.stringify(cache)) } catch { /* Storage may be disabled or full. */ }
}

const songCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })
function trackKey(track: Track) { return track.id || track.path || track.name }
function shuffledKeys(keys: string[]) {
  const result = [...keys]
  for (let index = result.length - 1; index > 0; index--) {
    const other = Math.floor(Math.random() * (index + 1))
    ;[result[index], result[other]] = [result[other], result[index]]
  }
  return result
}

function normalizationFactor(track: Track | undefined, enabled: boolean, catalog: boolean) {
  if (!enabled || !catalog || !Number.isFinite(track?.loudnessLufs)) return 1
  const delta = Math.max(-18, Math.min(12, -16 - track!.loudnessLufs!))
  return 10 ** (delta / 20)
}

function enablePlaybackAudioSession() {
  try {
    const session = (navigator as Navigator & { audioSession?: { type: string } }).audioSession
    if (session) session.type = 'playback'
  } catch { /* Browsers without Audio Session still use the media element. */ }
}

function searchText(value: string) {
  return value.normalize('NFD').replace(/\p{M}/gu, '').replace(/[đĐ]/g, 'd').toLocaleLowerCase()
}

function title(name: string) { return name.replace(/\.[^.]+$/, '').replace(/[_-]+/g, ' ') }
function time(value: number) {
  if (!Number.isFinite(value)) return '0:00'
  return `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, '0')}`
}

function CoverArt({ track, folder, large = false }: { track?: Track; folder: string; large?: boolean }) {
  const source = track?.id ? `/api/v1/music/cover?id=${encodeURIComponent(track.id)}` : track?.hasCover ? `/api/v1/music/cover?path=${encodeURIComponent(folder)}&name=${encodeURIComponent(track.name)}` : ''
  return <span className="music-art" aria-hidden="true">♫{source && <img key={source} src={source} alt="" loading={large ? 'eager' : 'lazy'} onError={(event) => { event.currentTarget.hidden = true }} />}</span>
}

export default function Music({ csrfToken, userId }: { csrfToken: string; userId: number }) {
  const [cached] = useState(() => readMusicCache(userId))
  const [path, setPath] = useState(() => cached?.root || localStorage.getItem('homeos.music.path') || '')
  const [folder, setFolder] = useState(() => cached?.root ?? '')
  const [tracks, setTracks] = useState<Track[]>(() => cached?.tracks ?? [])
  const [catalogMode, setCatalogMode] = useState(() => Boolean(cached?.root))
  const [libraryReady, setLibraryReady] = useState(false)
  const [activeID, setActiveID] = useState('')
  const [activeSnapshot, setActiveSnapshot] = useState<Track | undefined>()
  const [hasMore, setHasMore] = useState(() => cached?.hasMore ?? false)
  const [scanError, setScanError] = useState(() => cached?.scanError ?? '')
  const [personal, setPersonal] = useState<Personal | null>(null)
  const [collection, setCollection] = useState('library')
  const [playlistName, setPlaylistName] = useState('')
  const [showPlaylists, setShowPlaylists] = useState(false)
  const [showQueue, setShowQueue] = useState(false)
  const [showFilters, setShowFilters] = useState(false)
  const [artistFilter, setArtistFilter] = useState('')
  const [albumFilter, setAlbumFilter] = useState('')
  const [folderFilter, setFolderFilter] = useState('')
  const [playNonce, setPlayNonce] = useState(0)
  const [resumePosition, setResumePosition] = useState(0)
  const [query, setQuery] = useState('')
  const [mobileView, setMobileView] = useState<'list' | 'search'>('list')
  const [sortBy, setSortBy] = useState<SortKey | null>(null)
  const [sortDescending, setSortDescending] = useState(false)
  const [showFolderPicker, setShowFolderPicker] = useState(() => !cached?.root && !localStorage.getItem('homeos.music.path'))
  const [index, setIndex] = useState(-1)
  const [playing, setPlaying] = useState(false)
  const [playerRevealed, setPlayerRevealed] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(0.8)
  const [autoLevel, setAutoLevel] = useState(() => localStorage.getItem('homeos.music.autoLevel') !== 'off')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [deletingID, setDeletingID] = useState('')
  const [browserOpen, setBrowserOpen] = useState(false)
  const [browserLoading, setBrowserLoading] = useState(false)
  const [browserError, setBrowserError] = useState('')
  const [browserListing, setBrowserListing] = useState<FolderListing | null>(null)
  const audio = useRef<HTMLAudioElement>(null)
  const searchInput = useRef<HTMLInputElement>(null)
  const audioContext = useRef<AudioContext | null>(null)
  const normalizationGain = useRef<GainNode | null>(null)
  const masterGain = useRef<GainNode | null>(null)
  const lastVolume = useRef(0.8)
  const loadRequest = useRef(0)
  const lastPositionSave = useRef(0)
  const catalogQuery = useRef('')
  const shuffleScope = useRef('')
  const shuffleRemaining = useRef<string[]>([])
  const shuffleHistory = useRef<string[]>([])
  const deletedTrackID = useRef('')
  const active = tracks.find((track) => track.id && track.id === activeID) ?? activeSnapshot
  const search = searchText(query.trim())
  const visibleTracks = tracks.map((track, position) => ({ track, position })).filter(({ track }) =>
    [track.title, track.artist, track.album].some((value) => searchText(value).includes(search)))
  if (sortBy) visibleTracks.sort((left, right) => {
    const first = left.track[sortBy]
    const second = right.track[sortBy]
    if (!first && second) return 1
    if (first && !second) return -1
    const comparison = sortBy === 'duration' ? Number(first) - Number(second) : songCollator.compare(String(first), String(second))
    return (sortDescending ? -comparison : comparison) || left.position - right.position
  })

  useEffect(() => () => { void audioContext.current?.close() }, [])
  useEffect(() => {
    const gain = normalizationGain.current?.gain
    const context = audioContext.current
    if (!gain || !context) return
    gain.setTargetAtTime(normalizationFactor(active, autoLevel, catalogMode), context.currentTime, 0.15)
  }, [active?.id, active?.loudnessLufs, autoLevel, catalogMode])

  function ensureAudioGraph() {
    // iOS treats Web Audio as ambient unless the page opts into media playback.
    enablePlaybackAudioSession()
    if (audioContext.current) { void audioContext.current.resume(); return }
    if (!audio.current || !window.AudioContext) { if (audio.current) audio.current.volume = volume; return }
    try {
      const context = new AudioContext()
      const source = context.createMediaElementSource(audio.current)
      const gain = context.createGain()
      const compressor = context.createDynamicsCompressor()
      const master = context.createGain()
      compressor.threshold.value = -3
      compressor.knee.value = 6
      compressor.ratio.value = 12
      compressor.attack.value = 0.003
      compressor.release.value = 0.25
      gain.gain.value = normalizationFactor(active, autoLevel, catalogMode)
      source.connect(gain).connect(compressor).connect(master).connect(context.destination)
      master.gain.value = volume
      audio.current.volume = 1
      audioContext.current = context
      normalizationGain.current = gain
      masterGain.current = master
      void context.resume()
    } catch { if (audio.current) audio.current.volume = volume }
  }
  useEffect(() => {
    void (async () => {
      try {
        const response = await fetch('/api/v1/music/library')
        if (!response.ok) throw new Error('Could not load music library')
        const library = await response.json() as { root: string }
        if (library.root) {
          if (cached?.root && cached.root !== library.root) { setTracks([]); setHasMore(false); sessionStorage.removeItem(musicCacheKey(userId)) }
          setCatalogMode(true); setFolder(library.root); setPath(library.root); setShowFolderPicker(false); setLibraryReady(true); return
        }
      } catch {
        if (cached?.root) { setError('Could not check the music library. Showing the last saved list.'); return }
        /* SQLite installations still use the original folder API. */
      }
      if (cached) { setCatalogMode(false); setFolder(''); setTracks([]); sessionStorage.removeItem(musicCacheKey(userId)) }
      const saved = localStorage.getItem('homeos.music.path')
      if (saved) void loadFolder(saved)
    })()
  }, [])

  useEffect(() => { if (catalogMode) void loadPersonal() }, [catalogMode])

  async function loadPersonal(restore = true) {
    const response = await fetch('/api/v1/music/personal')
    if (!response.ok) return
    const data = await response.json() as Personal
    setPersonal(data)
    if (restore && data.player.trackId) {
      const trackResponse = await fetch(`/api/v1/music/tracks/${encodeURIComponent(data.player.trackId)}`)
      if (trackResponse.ok) {
        setActiveSnapshot(await trackResponse.json() as Track)
        setActiveID(data.player.trackId)
        setResumePosition(data.player.position)
      }
    }
  }

  async function postPersonal(endpoint: string, body: object) {
    const response = await fetch(`/api/v1/music/${endpoint}`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(body) })
    if (!response.ok) { const data = await response.json().catch(() => null); setError(data?.error?.message ?? 'Could not save music changes'); return false }
    return true
  }

  async function savePlayer(trackID = activeID, position = currentTime, nextPersonal = personal) {
    if (!catalogMode || !nextPersonal) return
    await postPersonal('player', { trackId: trackID, position, shuffle: nextPersonal.player.shuffle, repeat: nextPersonal.player.repeat })
  }

  async function toggleFavorite(track: Track) {
    if (!track.id || !personal) return
    const liked = !personal.favorites.includes(track.id)
    if (await postPersonal('favorite', { id: track.id, liked })) void loadPersonal(false)
  }

  async function createPlaylist() {
    if (!playlistName.trim()) return
    if (await postPersonal('playlists', { name: playlistName.trim() })) { setPlaylistName(''); void loadPersonal(false) }
  }

  async function addToPlaylist(playlist: Playlist, track: Track) {
    if (!track.id) return
    const ids = playlist.trackIds.includes(track.id) ? playlist.trackIds.filter((id) => id !== track.id) : [...playlist.trackIds, track.id]
    const response = await fetch(`/api/v1/music/playlists/${playlist.id}/tracks`, { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ trackIds: ids }) })
    if (response.ok) { void loadPersonal(false); if (collection === playlist.id) void refreshCatalog() }
    else setError('Could not update playlist')
  }

  async function reorderPlaylist(playlist: Playlist, position: number, direction: number) {
    const next = [...playlist.trackIds]
    const target = position + direction
    if (target < 0 || target >= next.length) return
    ;[next[position], next[target]] = [next[target], next[position]]
    const response = await fetch(`/api/v1/music/playlists/${playlist.id}/tracks`, { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ trackIds: next }) })
    if (response.ok) { void loadPersonal(false); if (collection === playlist.id) void refreshCatalog() }
    else setError('Could not reorder playlist')
  }

  async function deletePlaylist(playlist: Playlist) {
    const response = await fetch(`/api/v1/music/playlists/${playlist.id}`, { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } })
    if (response.ok) { if (collection === playlist.id) setCollection('library'); void loadPersonal(false) }
    else setError('Could not delete playlist')
  }

  async function deleteTrack(track: Track) {
    if (!track.id || !window.confirm(`Delete “${track.title || title(track.name)}” from the server disk?\n\nThis permanently removes ${track.path || track.name}.`)) return
    setDeletingID(track.id)
    try {
      const response = await fetch(`/api/v1/music/tracks/${encodeURIComponent(track.id)}`, { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } })
      if (!response.ok) {
        const data = await response.json().catch(() => null)
        throw new Error(data?.error?.message ?? 'Could not delete this song')
      }
      if (activeID === track.id) {
        deletedTrackID.current = track.id
        audio.current?.pause()
        setActiveID(''); setActiveSnapshot(undefined); setIndex(-1); setPlaying(false); setCurrentTime(0); setDuration(0); setResumePosition(0)
      }
      setTracks((current) => current.filter((item) => item.id !== track.id))
      const cache = readMusicCache(userId)
      if (cache?.root === folder) writeMusicCache(userId, { ...cache, tracks: cache.tracks.filter((item) => item.id !== track.id) })
      setPersonal((current) => current ? { ...current, queue: current.queue.filter((id) => id !== track.id), player: current.player.trackId === track.id ? { ...current.player, trackId: '', position: 0 } : current.player } : current)
      setError('')
      void refreshCatalog()
      void loadPersonal(false)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not delete this song')
    } finally {
      setDeletingID('')
    }
  }

  async function setQueue(ids: string[]) {
    if (await postPersonal('queue', { trackIds: ids })) setPersonal((current) => current ? { ...current, queue: ids } : current)
  }

  useEffect(() => {
    if (!catalogMode || !folder || !libraryReady) return
    const timer = window.setTimeout(() => void refreshCatalog(), query.trim() ? 250 : 0)
    const interval = window.setInterval(() => void refreshCatalog(), 15000)
    return () => { window.clearTimeout(timer); window.clearInterval(interval) }
  }, [catalogMode, folder, libraryReady, query, sortBy, sortDescending, collection, personal?.recent, artistFilter, albumFilter, folderFilter])

  async function refreshCatalog(offset = 0) {
    if (collection !== 'library') {
      catalogQuery.current = ''
      if (collection === 'recent') { const seen = new Set<string>(); setTracks(personal?.recent.filter((track) => { if (!track.available || !track.id || seen.has(track.id)) return false; seen.add(track.id); return true }) ?? []); setHasMore(false); return }
      const endpoint = collection === 'favorites' ? '/api/v1/music/favorites' : `/api/v1/music/playlists/${encodeURIComponent(collection)}/tracks`
      const response = await fetch(endpoint)
      if (response.ok) { const data = await response.json() as { tracks: Track[] }; setTracks(data.tracks); setHasMore(false) }
      return
    }
    const params = new URLSearchParams({ limit: '500', offset: String(offset) })
    if (query.trim()) params.set('q', query.trim())
    if (artistFilter.trim()) params.set('artist', artistFilter.trim())
    if (albumFilter.trim()) params.set('album', albumFilter.trim())
    if (folderFilter.trim()) params.set('folder', folderFilter.trim())
    if (sortBy) { params.set('sort', sortBy); params.set('order', sortDescending ? 'desc' : 'asc') }
    const key = `${query}|${artistFilter}|${albumFilter}|${folderFilter}|${sortBy}|${sortDescending}`
    const changed = catalogQuery.current !== key
    catalogQuery.current = key
    const response = await fetch(`/api/v1/music?${params}`)
    if (!response.ok) { setError('Could not refresh music library'); return }
    const data = await response.json() as { tracks: Track[]; scanError?: string }
    setTracks((previous) => {
      if (changed || previous.length <= 500) return offset ? [...previous, ...data.tracks] : data.tracks
      const seen = new Set(data.tracks.map((track) => track.id ?? track.path ?? track.name))
      return offset ? [...previous, ...data.tracks.filter((track) => !previous.some((item) => item.id === track.id))] : [...data.tracks, ...previous.filter((track) => !seen.has(track.id ?? track.path ?? track.name))]
    })
    setHasMore(data.tracks.length === 500)
    setScanError(data.scanError ?? '')
    if (offset === 0 && !query.trim() && !artistFilter.trim() && !albumFilter.trim() && !folderFilter.trim() && !sortBy) {
      writeMusicCache(userId, { root: folder, tracks: data.tracks, hasMore: data.tracks.length === 500, scanError: data.scanError ?? '' })
    }
  }

  function changeVolume(next: number) {
    setVolume(next)
    if (next > 0) lastVolume.current = next
    if (masterGain.current) masterGain.current.gain.value = next
    else if (audio.current) audio.current.volume = next
  }

  async function loadFolder(next: string) {
    if (!next) return
    try {
      const response = await fetch('/api/v1/music/library', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ root: next }) })
      if (response.ok) {
        const data = await response.json() as { root: string }
        audio.current?.pause(); setPlaying(false); setActiveID(''); setActiveSnapshot(undefined); setIndex(-1)
        sessionStorage.removeItem(musicCacheKey(userId))
        setCatalogMode(true); setLibraryReady(true); setFolder(data.root); setPath(data.root); setTracks([]); setShowFolderPicker(false); setBrowserOpen(false); setError('')
        setCollection('library'); void loadPersonal(false)
        localStorage.setItem('homeos.music.path', data.root)
        return
      }
      const body = await response.json().catch(() => null)
      if (body?.error?.code !== 'postgres_required') throw new Error(body?.error?.message ?? 'Could not set music folder')
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not set music folder'); setShowFolderPicker(true); return }
    await loadLegacyFolder(next)
  }

  async function loadLegacyFolder(next: string) {
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
      setActiveID('')
      setActiveSnapshot(undefined)
      setCurrentTime(0)
      setDuration(0)
      setTracks(data.tracks)
      setCatalogMode(false)
      setLibraryReady(false)
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

  function ensureShufflePool() {
    const keys = visibleTracks.map(({ track }) => trackKey(track))
    const scope = [...keys].sort().join('\u0000')
    if (shuffleScope.current !== scope) {
      shuffleScope.current = scope
      shuffleRemaining.current = shuffledKeys(keys.filter((key) => key !== (active && trackKey(active))))
      shuffleHistory.current = []
    }
  }

  function playTrack(next: number, remember = true) {
    if (next < 0 || next >= tracks.length) return
    ensureAudioGraph()
    if (tracks[next].id ? tracks[next].id === activeID : next === index) {
      if (audio.current?.paused) void audio.current.play().catch(() => setError('This audio format could not be played.'))
      return
    }
    if (personal?.player.shuffle) {
      ensureShufflePool()
      if (remember && active) shuffleHistory.current.push(trackKey(active))
      shuffleRemaining.current = shuffleRemaining.current.filter((key) => key !== trackKey(tracks[next]))
    }
    setError('')
    setIndex(next)
    setActiveID(tracks[next].id ?? '')
    setActiveSnapshot(tracks[next])
    setResumePosition(0)
    setPlayNonce((value) => value + 1)
    if (tracks[next].id) void postPersonal('play', { id: tracks[next].id }).then(() => loadPersonal(false))
    setCurrentTime(0)
    setDuration(0)
  }

  useEffect(() => {
    if (playNonce === 0) return
    void audio.current?.play().catch(() => setError('This audio format could not be played.'))
  }, [playNonce])

  function skip(direction: number) {
    ensureAudioGraph()
    if (direction > 0 && personal?.queue.length) {
      const nextID = personal.queue[0]
      const queued = tracks.findIndex((track) => track.id === nextID)
      void setQueue(personal.queue.slice(1))
      if (queued >= 0) { playTrack(queued); return }
      void fetch(`/api/v1/music/tracks/${encodeURIComponent(nextID)}`).then(async (response) => {
        if (!response.ok) { setError('Queued song is unavailable'); return }
        const track = await response.json() as Track
        setActiveSnapshot(track); setActiveID(nextID); setIndex(-1); setResumePosition(0); setPlayNonce((value) => value + 1)
        void postPersonal('play', { id: nextID }).then(() => loadPersonal(false))
      })
      return
    }
    if (!visibleTracks.length) return
    if (personal?.player.shuffle) {
      ensureShufflePool()
      if (direction < 0) {
        while (shuffleHistory.current.length) {
          const previous = shuffleHistory.current.pop()!
          const item = visibleTracks.find(({ track }) => trackKey(track) === previous)
          if (item) {
            if (active) shuffleRemaining.current.push(trackKey(active))
            playTrack(item.position, false)
            return
          }
        }
        return
      }
      if (!shuffleRemaining.current.length) {
        if (personal.player.repeat !== 'all') return
        const keys = visibleTracks.map(({ track }) => trackKey(track))
        shuffleRemaining.current = shuffledKeys(keys.length > 1 ? keys.filter((key) => key !== (active && trackKey(active))) : keys)
      }
      const next = shuffleRemaining.current.pop()
      const item = visibleTracks.find(({ track }) => trackKey(track) === next)
      if (item) playTrack(item.position)
      return
    }
    const current = visibleTracks.findIndex((item) => item.track.id ? item.track.id === activeID : item.position === index)
    const next = current < 0 ? 0 : (current + direction + visibleTracks.length) % visibleTracks.length
    playTrack(visibleTracks[next].position)
  }

  function songEnded() {
    if (personal?.player.repeat === 'one' && audio.current) { audio.current.currentTime = 0; void audio.current.play(); return }
    const current = visibleTracks.findIndex((item) => item.track.id ? item.track.id === activeID : item.position === index)
    if (!personal?.player.shuffle && personal?.player.repeat === 'off' && current === visibleTracks.length - 1 && !personal.queue.length) return
    skip(1)
  }

  const mediaActions = useRef({
    play: () => {}, pause: () => {}, previous: () => {}, next: () => {}, seek: (_position: number) => {},
  })
  mediaActions.current = {
    play: () => { ensureAudioGraph(); void audio.current?.play().catch(() => setError('This audio format could not be played.')) },
    pause: () => audio.current?.pause(),
    previous: () => skip(-1),
    next: () => skip(1),
    seek: (position: number) => { if (audio.current) audio.current.currentTime = position },
  }
  useEffect(() => {
    if (!('mediaSession' in navigator)) return
    const session = navigator.mediaSession
    const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      ['play', () => mediaActions.current.play()],
      ['pause', () => mediaActions.current.pause()],
      ['previoustrack', () => mediaActions.current.previous()],
      ['nexttrack', () => mediaActions.current.next()],
      ['seekto', (details) => { if (details.seekTime != null) mediaActions.current.seek(details.seekTime) }],
    ]
    for (const [action, handler] of handlers) {
      try { session.setActionHandler(action, handler) } catch { /* This action is unsupported. */ }
    }
    return () => { for (const [action] of handlers) { try { session.setActionHandler(action, null) } catch { /* Unsupported action. */ } } }
  }, [])
  useEffect(() => {
    if (!('mediaSession' in navigator)) return
    navigator.mediaSession.metadata = active && 'MediaMetadata' in window ? new MediaMetadata({
      title: active.title || title(active.name), artist: active.artist || 'Unknown artist', album: active.album,
      artwork: active.hasCover ? [{ src: new URL(active.id ? `/api/v1/music/cover?id=${encodeURIComponent(active.id)}` : `/api/v1/music/cover?path=${encodeURIComponent(folder)}&name=${encodeURIComponent(active.name)}`, location.href).href }] : [],
    }) : null
  }, [active?.id, active?.name, active?.title, active?.artist, active?.album, active?.hasCover, folder])
  useEffect(() => {
    if ('mediaSession' in navigator) navigator.mediaSession.playbackState = playing ? 'playing' : 'paused'
  }, [playing])
  useEffect(() => {
    if (!('mediaSession' in navigator) || !Number.isFinite(duration) || duration <= 0) return
    try { navigator.mediaSession.setPositionState({ duration, position: Math.min(Math.max(currentTime, 0), duration) }) } catch { /* Position controls are optional. */ }
  }, [currentTime, duration])
  useEffect(() => {
    const resumeOnReturn = () => {
      if (document.visibilityState === 'visible' && audioContext.current && !audio.current?.paused) void audioContext.current.resume().catch(() => {})
    }
    document.addEventListener('visibilitychange', resumeOnReturn)
    return () => document.removeEventListener('visibilitychange', resumeOnReturn)
  }, [])

  async function toggleMode(mode: 'shuffle' | 'repeat') {
    if (!personal) return
    if (mode === 'shuffle') { shuffleScope.current = ''; shuffleRemaining.current = []; shuffleHistory.current = [] }
    const player = { ...personal.player, shuffle: mode === 'shuffle' ? !personal.player.shuffle : personal.player.shuffle, repeat: mode === 'repeat' ? personal.player.repeat === 'off' ? 'all' as const : personal.player.repeat === 'all' ? 'one' as const : 'off' as const : personal.player.repeat }
    const next = { ...personal, player }
    setPersonal(next)
    await savePlayer(activeID, currentTime, next)
  }

  function selectMobileView(view: 'list' | 'search') {
    setMobileView(view)
    if (view === 'list') setQuery('')
    else window.requestAnimationFrame(() => searchInput.current?.focus())
  }

  return <section className="music" aria-label="HomeMusic player" data-mobile-view={mobileView} data-search-empty={!query.trim()} data-player-visible={playerRevealed && Boolean(active)}>
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
      {scanError && <p className="music-error" role="status">Library scan: {scanError}</p>}
      <div className="music-library-toolbar">
      {folder && <div className="music-search"><span aria-hidden="true">⌕</span><input ref={searchInput} type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search title, artist, or album" aria-label="Search songs by title, artist, or album" /></div>}
      <div className={`music-heading ${showFolderPicker ? '' : 'is-compact'}`}>
        <div><p className="eyebrow">Your library</p><h2>{folder ? collection === 'library' ? 'Music' : collection === 'favorites' ? 'Liked songs' : collection === 'recent' ? 'Recently played' : personal?.playlists.find((item) => item.id === collection)?.name ?? 'Playlist' : loading ? 'Loading music…' : 'Choose a folder'}</h2><span className="music-mobile-count">{tracks.length}{hasMore ? '+' : ''} {tracks.length === 1 ? 'track' : 'tracks'}</span></div>
        <div className="music-heading-actions"><span>{tracks.length}{hasMore ? '+' : ''} {tracks.length === 1 ? 'track' : 'tracks'}</span>{catalogMode && <><button type="button" aria-pressed={collection === 'library'} onClick={() => setCollection('library')}>All songs</button><button type="button" aria-pressed={collection === 'favorites'} onClick={() => setCollection('favorites')}>Liked</button><button type="button" aria-pressed={collection === 'recent'} onClick={() => setCollection('recent')}>Recent</button><button type="button" aria-pressed={showPlaylists || !['library', 'favorites', 'recent'].includes(collection)} onClick={() => setShowPlaylists((value) => !value)}>Playlists</button><button type="button" aria-pressed={showFilters} onClick={() => setShowFilters((value) => !value)}>Filters</button></>}{folder && <button type="button" className="secondary-button music-change-folder" onClick={() => setShowFolderPicker((current) => !current)}>{showFolderPicker ? 'Hide folder controls' : 'Change folder'}</button>}</div>
      </div>
      {folder && <p className="music-path" title={folder}>{folder}</p>}
      </div>
      {folder && <p className="music-mobile-search-prompt">Search songs by title, artist, or album.</p>}
      {catalogMode && showFilters && <div className="music-filters"><input aria-label="Filter by artist" placeholder="Artist" value={artistFilter} onChange={(event) => setArtistFilter(event.target.value)} /><input aria-label="Filter by album" placeholder="Album" value={albumFilter} onChange={(event) => setAlbumFilter(event.target.value)} /><input aria-label="Filter by folder" placeholder="Folder path" value={folderFilter} onChange={(event) => setFolderFilter(event.target.value)} /></div>}
      {catalogMode && showPlaylists && <div className="music-playlists">
        <div className="music-playlist-create"><input aria-label="New playlist name" value={playlistName} onChange={(event) => setPlaylistName(event.target.value)} placeholder="New playlist name" /><button type="button" onClick={() => void createPlaylist()}>Create playlist</button></div>
        {personal?.playlists.map((playlist) => <div key={playlist.id}><div className="music-playlist-item"><button type="button" onClick={() => { setSortBy(null); setCollection(playlist.id) }}>{playlist.name} <small>{playlist.trackIds.length} songs</small></button>{active?.id && <button type="button" onClick={() => void addToPlaylist(playlist, active)}>{playlist.trackIds.includes(active.id) ? 'Remove current' : 'Add current'}</button>}<button type="button" aria-label={`Delete ${playlist.name}`} onClick={() => void deletePlaylist(playlist)}>✕</button></div>{collection === playlist.id && playlist.trackIds.map((id, position) => <div className="music-playlist-item music-playlist-order" key={id}><span>{position + 1}. {tracks.find((track) => track.id === id)?.title ?? id}</span><button type="button" aria-label="Move song up" disabled={position === 0} onClick={() => void reorderPlaylist(playlist, position, -1)}>↑</button><button type="button" aria-label="Move song down" disabled={position === playlist.trackIds.length - 1} onClick={() => void reorderPlaylist(playlist, position, 1)}>↓</button></div>)}</div>)}
      </div>}
      {catalogMode && showQueue && <div className="music-playlists"><strong>Up next</strong>{personal?.queue.map((id, position) => <div className="music-playlist-item" key={`${id}-${position}`}><span>{tracks.find((track) => track.id === id)?.title ?? id}</span><button type="button" aria-label="Move queued song up" disabled={position === 0} onClick={() => { const next = [...personal.queue]; [next[position - 1], next[position]] = [next[position], next[position - 1]]; void setQueue(next) }}>↑</button><button type="button" aria-label="Move queued song down" disabled={position === personal.queue.length - 1} onClick={() => { const next = [...personal.queue]; [next[position + 1], next[position]] = [next[position], next[position + 1]]; void setQueue(next) }}>↓</button><button type="button" onClick={() => void setQueue(personal.queue.filter((_, index) => index !== position))}>Remove</button></div>)}{!personal?.queue.length && <small>Queue is empty.</small>}</div>}
      <div className="music-table-head"><span>#</span><button type="button" className="music-sort-button" onClick={() => changeSort('title')} aria-label={`Sort by title${sortBy === 'title' ? `, ${sortDescending ? 'descending' : 'ascending'}` : ''}`}>Title {sortBy === 'title' && (sortDescending ? '↓' : '↑')}</button><button type="button" className="music-sort-button" onClick={() => changeSort('artist')} aria-label={`Sort by artist${sortBy === 'artist' ? `, ${sortDescending ? 'descending' : 'ascending'}` : ''}`}>Artist {sortBy === 'artist' && (sortDescending ? '↓' : '↑')}</button><span>Album</span><button type="button" className="music-sort-button music-sort-duration" onClick={() => changeSort('duration')} aria-label={`Sort by duration${sortBy === 'duration' ? `, ${sortDescending ? 'descending' : 'ascending'}` : ''}`}>◷ {sortBy === 'duration' && (sortDescending ? '↓' : '↑')}</button></div>
      <ol className="music-list">
        {visibleTracks.map(({ track, position }, rowNumber) => <li className="music-list-item" key={track.id ?? track.name}>
          <button className={`music-row ${(track.id ? activeID === track.id : index === position) ? 'is-current' : ''}`} type="button" onClick={() => playTrack(position)} aria-label={`Play ${track.title || title(track.name)}`}>
            <span className="music-number">{(track.id ? activeID === track.id : index === position) && playing ? <span className="music-equalizer" aria-hidden="true"><i /><i /><i /></span> : rowNumber + 1}</span>
            <span className="music-track"><CoverArt track={track} folder={folder} /><span><strong>{track.title || title(track.name)}</strong><small>{track.artist || 'Unknown artist'} · {track.album || 'Unknown album'}{track.id && personal?.playCounts[track.id] ? ` · ${personal.playCounts[track.id]} plays` : ''}</small></span></span>
            <span className="music-artist">{track.artist || 'Unknown artist'}</span>
            <span className="music-album">{track.album || 'Unknown album'}</span>
            <span className="music-duration">{track.duration > 0 ? time(track.duration) : '—'}</span>
          </button>
          {catalogMode && track.id && <button type="button" className="music-delete-track" aria-label={`Delete ${track.title || title(track.name)} from disk`} title="Delete song from disk" disabled={deletingID === track.id} onClick={() => void deleteTrack(track)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M9 7V4h6v3m-9 0 1 13h10l1-13M10 11v5m4-5v5" /></svg></button>}
        </li>)}
      </ol>
      {catalogMode && hasMore && <button type="button" className="music-load-more" onClick={() => void refreshCatalog(tracks.length)}>Load more songs</button>}
      {folder && tracks.length === 0 && <p className="music-empty">{catalogMode ? 'Indexing music, or no supported audio files were found.' : 'No supported audio files in this folder.'}</p>}
      {folder && tracks.length > 0 && visibleTracks.length === 0 && <p className="music-empty">No songs match “{query}”.</p>}
      {!folder && !loading && <p className="music-empty">Enter a folder path above to start listening.</p>}
    </div>
    <div className="music-player">
      <div className="music-now"><CoverArt track={active} folder={folder} large /><span><strong>{active ? active.title || title(active.name) : 'Nothing playing'}</strong><small>{active ? active.artist || 'Unknown artist' : 'Select a track'}</small></span>{catalogMode && active?.id && <button className="music-like" type="button" aria-label={personal?.favorites.includes(active.id) ? 'Unlike song' : 'Like song'} onClick={() => void toggleFavorite(active)}>{personal?.favorites.includes(active.id) ? '♥' : '♡'}</button>}</div>
      <div className="music-playback">
        <div className="music-controls">
          {catalogMode && <button type="button" className="music-shuffle-button" aria-label="Shuffle" title={personal?.player.shuffle ? 'Shuffle on' : 'Shuffle off'} aria-pressed={personal?.player.shuffle ?? false} disabled={!personal} onClick={() => void toggleMode('shuffle')}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h3c4 0 6 10 10 10h3m-3-3 3 3-3 3M4 17h3c1.5 0 2.7-1.3 3.8-3M14 10c1-1.8 2-3 3-3h3m-3-3 3 3-3 3" /></svg><span>Shuffle</span></button>}
          <button type="button" aria-label="Previous track" onClick={() => skip(-1)} disabled={!visibleTracks.length}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 5v14M19 5 7 12l12 7z" /></svg></button>
          <button type="button" className="music-play-button" aria-label={playing ? 'Pause' : 'Play'} onClick={() => { if (!active && visibleTracks.length) { if (personal?.player.shuffle) skip(1); else playTrack(visibleTracks[0].position) } else if (playing) audio.current?.pause(); else { ensureAudioGraph(); void audio.current?.play().catch(() => setError('This audio format could not be played.')) } }} disabled={!active && !visibleTracks.length}>{playing ? <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 5v14M17 5v14" /></svg> : <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m8 5 11 7-11 7z" /></svg>}</button>
          <button type="button" aria-label="Next track" onClick={() => skip(1)} disabled={!visibleTracks.length && !personal?.queue.length}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M19 5v14M5 5l12 7-12 7z" /></svg></button>
          {catalogMode && <button type="button" aria-label={`Repeat ${personal?.player.repeat ?? 'off'}`} aria-pressed={personal ? personal.player.repeat !== 'off' : false} onClick={() => void toggleMode('repeat')}>↻{personal?.player.repeat === 'one' ? '₁' : ''}</button>}
        </div>
        <div className="music-timeline"><span>{time(currentTime)}</span><input type="range" aria-label="Seek" min="0" max={duration || 1} step="0.1" value={Math.min(currentTime, duration || 1)} disabled={!active || !duration} onChange={(event) => { if (audio.current) audio.current.currentTime = Number(event.target.value); setCurrentTime(Number(event.target.value)) }} style={{ '--progress': `${duration ? currentTime / duration * 100 : 0}%` } as React.CSSProperties} /><span>{time(duration)}</span></div>
      </div>
      <div className="music-player-actions">{catalogMode && <><button type="button" aria-label="Show queue" onClick={() => setShowQueue((value) => !value)}>≡</button>{active?.id && <button type="button" aria-label="Add current song to queue" onClick={() => void setQueue([...(personal?.queue ?? []), active.id!])}>+</button>}<button type="button" className="music-auto-level" aria-label="Auto level" aria-pressed={autoLevel} title={autoLevel ? active?.id && active.loudnessLufs == null ? 'Auto level on; measuring this song in the background' : 'Auto level on' : 'Auto level off'} onClick={() => { setAutoLevel((current) => { localStorage.setItem('homeos.music.autoLevel', current ? 'off' : 'on'); return !current }) }}>Auto</button></>}<div className="music-volume"><button type="button" aria-label={volume === 0 ? 'Unmute' : 'Mute'} title={volume === 0 ? 'Unmute' : 'Mute'} onClick={() => changeVolume(volume === 0 ? lastVolume.current : 0)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 9v6h4l5 4V5L7 9z" />{volume === 0 ? <path d="m16 9 5 6m0-6-5 6" /> : <path d="M16 8c2 1 3 2 3 4s-1 3-3 4" />}</svg></button><input type="range" aria-label="Volume" min="0" max="1" step="0.01" value={volume} onChange={(event) => changeVolume(Number(event.target.value))} style={{ '--volume': `${volume * 100}%` } as React.CSSProperties} /></div></div>
    </div>
    <audio ref={audio} src={active ? active.id ? `/api/v1/music/audio?id=${encodeURIComponent(active.id)}` : `/api/v1/music/audio?path=${encodeURIComponent(folder)}&name=${encodeURIComponent(active.name)}` : undefined} preload="metadata" onLoadedMetadata={(event) => { if (resumePosition > 0) { event.currentTarget.currentTime = Math.min(resumePosition, Math.max(0, event.currentTarget.duration - 1)); setResumePosition(0) } }} onTimeUpdate={(event) => { const position = event.currentTarget.currentTime; setCurrentTime(position); if (catalogMode && active?.id && active.id !== deletedTrackID.current && Date.now() - lastPositionSave.current > 10000) { lastPositionSave.current = Date.now(); void savePlayer(active.id, position) } }} onDurationChange={(event) => setDuration(event.currentTarget.duration)} onPlay={(event) => { setPlaying(true); setPlayerRevealed(true); if (catalogMode && active?.id && active.id !== deletedTrackID.current) void savePlayer(active.id, event.currentTarget.currentTime) }} onPause={(event) => { setPlaying(false); if (catalogMode && active?.id && active.id !== deletedTrackID.current && event.currentTarget.currentSrc && new URL(event.currentTarget.currentSrc).searchParams.get('id') === active.id) void savePlayer(active.id, event.currentTarget.currentTime) }} onEnded={songEnded} onError={() => { if (active) setError('This track could not be loaded or played.') }} />
    <nav className="music-mobile-nav" aria-label="HomeMusic mobile navigation">
      <button type="button" className={mobileView === 'list' ? 'active' : ''} aria-current={mobileView === 'list' ? 'page' : undefined} onClick={() => selectMobileView('list')}><span aria-hidden="true">☷</span>List</button>
      <button type="button" className={mobileView === 'search' ? 'active' : ''} aria-current={mobileView === 'search' ? 'page' : undefined} onClick={() => selectMobileView('search')}><span aria-hidden="true">⌕</span>Search</button>
    </nav>
  </section>
}
