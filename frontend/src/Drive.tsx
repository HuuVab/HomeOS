import { useCallback, useEffect, useRef, useState, type DragEvent } from 'react'

type Entry = { name: string; path: string; kind: 'file' | 'folder'; size: number; modifiedAt: string; version?: number }
type TrashEntry = { id: string; path: string; deletedAt: string }
type StorageUsage = { usedBytes: number; reservedBytes: number; quotaBytes: number; maxFileBytes: number }
type Props = { csrfToken: string }
type SortKey = 'name' | 'date' | 'size' | 'type'
type Action = 'rename' | 'move' | 'copy' | 'trash'

const chunkSize = 4 * 1024 * 1024
const textFile = /\.(txt|md|csv|json|log|go|js|ts|tsx|py|css)$/i
const imageFile = /\.(png|jpe?g|gif|webp)$/i
const previewable = /\.(png|jpe?g|gif|webp|pdf|txt|md|csv|json|log|go|js|ts|tsx|py|css)$/i

function bytes(size: number): string {
  if (size < 1024) return `${size} B`
  const unit = Math.min(Math.floor(Math.log(size) / Math.log(1024)), 4)
  return `${(size / 1024 ** unit).toFixed(unit === 1 ? 0 : 1)} ${['B', 'KB', 'MB', 'GB', 'TB'][unit]}`
}

function modified(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  const days = Math.floor((Date.now() - date.getTime()) / 86400000)
  if (days === 0) return 'Today'
  if (days === 1) return 'Yesterday'
  if (days < 7) return `${days} days ago`
  return date.toLocaleDateString()
}

function fileType(entry: Entry): string {
  return entry.kind === 'folder' ? 'folder' : entry.name.split('.').pop()?.toLowerCase() ?? 'file'
}

function EntryIcon({ entry }: { entry: Entry }) {
  if (entry.kind === 'folder') return <svg className="drive-icon folder-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M2 5.5A2.5 2.5 0 0 1 4.5 3h5l2.3 2.5h7.7A2.5 2.5 0 0 1 22 8v11a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2z" /></svg>
  if (imageFile.test(entry.name)) return <span className="drive-icon image-icon" aria-hidden="true">▧</span>
  if (/\.pdf$/i.test(entry.name)) return <span className="drive-icon pdf-icon" aria-hidden="true">PDF</span>
  return <svg className="drive-icon file-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 2h9l5 5v14a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1z" /><path d="M14 2v5h5M7 13h9M7 17h9" /></svg>
}

async function fingerprint(file: File): Promise<string> {
  const first = await file.slice(0, 64 * 1024).arrayBuffer()
  const last = await file.slice(Math.max(0, file.size - 64 * 1024)).arrayBuffer()
  const sample = new Uint8Array(first.byteLength + last.byteLength + 8)
  sample.set(new Uint8Array(first))
  sample.set(new Uint8Array(last), first.byteLength)
  new DataView(sample.buffer).setBigUint64(sample.length - 8, BigInt(file.size))
  const hash = await crypto.subtle.digest('SHA-256', sample)
  return Array.from(new Uint8Array(hash), (byte) => byte.toString(16).padStart(2, '0')).join('')
}

async function responseError(response: Response): Promise<Error> {
  const code = (await response.json().catch(() => null))?.error?.code
  if (code === 'quota_exceeded') return new Error('Storage quota exceeded. Free space or change the server quota.')
  if (response.status === 413) return new Error('File exceeds the configured size limit.')
  if (response.status === 412 || response.status === 428) return new Error('This item changed. Refresh the file list and try again.')
  if (response.status === 409) return new Error('A file or folder already exists at that location.')
  return new Error(`HomeDrive request failed (${response.status}).`)
}

export default function Drive({ csrfToken }: Props) {
  const [folder, setFolder] = useState('')
  const [entries, setEntries] = useState<Entry[]>([])
  const [trashEntries, setTrashEntries] = useState<TrashEntry[]>([])
  const [storage, setStorage] = useState<StorageUsage | null>(null)
  const [query, setQuery] = useState('')
  const [searchEntries, setSearchEntries] = useState<Entry[]>([])
  const [searching, setSearching] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [uploads, setUploads] = useState<File[]>([])
  const [uploadName, setUploadName] = useState('')
  const [progress, setProgress] = useState<number | null>(null)
  const [showTrash, setShowTrash] = useState(false)
  const [view, setView] = useState<'list' | 'grid'>('list')
  const [sortBy, setSortBy] = useState<SortKey>('name')
  const [descending, setDescending] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [openMenu, setOpenMenu] = useState<string | null>(null)
  const [newMenu, setNewMenu] = useState(false)
  const [dragActive, setDragActive] = useState(false)
  const [inspected, setInspected] = useState<Entry | null>(null)
  const [previewText, setPreviewText] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async (signal?: AbortSignal) => {
    const response = await fetch(`/api/v1/drive?path=${encodeURIComponent(folder)}`, { cache: 'no-store', signal })
    if (!response.ok) throw await responseError(response)
    const data: { entries: Entry[] } = await response.json()
    if (!signal?.aborted) setEntries(data.entries)
  }, [folder])

  const loadStorage = useCallback(async () => {
    const response = await fetch('/api/v1/drive/storage', { cache: 'no-store' })
    if (response.ok) setStorage(await response.json() as StorageUsage)
  }, [])

  const loadTrash = useCallback(async () => {
    const response = await fetch('/api/v1/drive/trash', { cache: 'no-store' })
    if (!response.ok) throw await responseError(response)
    const data: { entries: TrashEntry[] } = await response.json()
    setTrashEntries(data.entries)
  }, [])

  const refresh = useCallback(async () => {
    await Promise.all([load(), loadStorage(), loadTrash()])
  }, [load, loadStorage, loadTrash])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    void load(controller.signal).then(() => setError(null)).catch((cause) => {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Unable to load files')
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [load])

  useEffect(() => { void loadStorage() }, [loadStorage])
  useEffect(() => { if (showTrash) void loadTrash().catch((cause) => setError(String(cause))) }, [showTrash, loadTrash])

  useEffect(() => {
    if (!query.trim()) { setSearchEntries([]); setSearching(false); return }
    const controller = new AbortController()
    setSearching(true)
    const timer = window.setTimeout(async () => {
      try {
        const response = await fetch(`/api/v1/drive/search?q=${encodeURIComponent(query.trim())}`, { cache: 'no-store', signal: controller.signal })
        if (!response.ok) throw await responseError(response)
        const data: { entries: Entry[] } = await response.json()
        if (!controller.signal.aborted) setSearchEntries(data.entries)
      } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Search failed') }
      finally { if (!controller.signal.aborted) setSearching(false) }
    }, 250)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [query])

  useEffect(() => {
    setPreviewText(null)
    if (!inspected || !textFile.test(inspected.name)) return
    const controller = new AbortController()
    void fetch(`/api/v1/drive/preview?path=${encodeURIComponent(inspected.path)}`, { signal: controller.signal })
      .then(async (response) => { if (!response.ok) throw await responseError(response); return response.text() })
      .then((content) => { if (!controller.signal.aborted) setPreviewText(content) })
      .catch((cause) => { if (!controller.signal.aborted) setPreviewText(cause instanceof Error ? cause.message : 'Preview unavailable') })
    return () => controller.abort()
  }, [inspected])

  const visible = (query.trim() ? searchEntries : entries).slice().sort((a, b) => {
    const comparison = sortBy === 'date' ? a.modifiedAt.localeCompare(b.modifiedAt)
      : sortBy === 'size' ? a.size - b.size
      : sortBy === 'type' ? fileType(a).localeCompare(fileType(b)) || a.name.localeCompare(b.name)
      : a.name.localeCompare(b.name)
    return descending ? -comparison : comparison
  })
  const quickAccess = entries.filter((entry) => entry.kind === 'file').slice().sort((a, b) => b.modifiedAt.localeCompare(a.modifiedAt)).slice(0, 6)
  const selectedEntries = visible.filter((entry) => selected.has(entry.path))

  function toggleSelected(path: string) {
    setSelected((current) => { const next = new Set(current); if (next.has(path)) next.delete(path); else next.add(path); return next })
  }

  function navigate(path: string) { setFolder(path); setQuery(''); setSelected(new Set()); setInspected(null); setOpenMenu(null) }
  function openEntry(entry: Entry) { if (entry.kind === 'folder') navigate(entry.path); else setInspected(entry) }

  async function mutate(endpoint: string, payload: object, version?: number): Promise<void> {
    const response = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken, ...(version ? { 'X-Source-Version': String(version) } : {}) }, body: JSON.stringify(payload) })
    if (!response.ok) throw await responseError(response)
  }

  async function createFolder() {
    setNewMenu(false)
    const name = window.prompt('Folder name')?.trim()
    if (!name) return
    setBusy(true)
    try { await mutate('/api/v1/drive/folders', { path: folder ? `${folder}/${name}` : name }); await refresh(); setError(null) }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not create folder') }
    finally { setBusy(false) }
  }

  async function changeEntry(entry: Entry, action: Action) {
    setOpenMenu(null)
    let endpoint = '/api/v1/drive/trash'
    let payload: object = { path: entry.path }
    if (action !== 'trash') {
      const proposed = window.prompt(action === 'rename' ? 'New name' : 'Destination path from drive root', action === 'rename' ? entry.name : action === 'copy' ? `${entry.path}-copy` : entry.path)
      if (proposed === null) return
      const to = action === 'rename' ? [...entry.path.split('/').slice(0, -1), proposed.trim()].filter(Boolean).join('/') : proposed.trim()
      endpoint = action === 'copy' ? '/api/v1/drive/copy' : '/api/v1/drive/move'
      payload = { from: entry.path, to }
    } else if (!window.confirm(`Move ${entry.name} to Trash?`)) return
    setBusy(true)
    try { await mutate(endpoint, payload, entry.version); setSelected((current) => { const next = new Set(current); next.delete(entry.path); return next }); await refresh(); setError(null) }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not change item') }
    finally { setBusy(false) }
  }

  async function bulkAction(action: 'trash' | 'move' | 'copy') {
    if (selectedEntries.length === 0) return
    let destination = ''
    if (action === 'trash') { if (!window.confirm(`Move ${selectedEntries.length} items to Trash?`)) return }
    else { const proposed = window.prompt('Destination folder path from drive root (leave blank for root)', ''); if (proposed === null) return; destination = proposed.trim().replace(/\/$/, ''); if (selectedEntries.some((entry) => (destination ? `${destination}/${entry.name}` : entry.name) === entry.path)) { setError('Choose a destination different from the current folder.'); return } }
    setBusy(true)
    try {
      for (const entry of selectedEntries) {
        await mutate(action === 'trash' ? '/api/v1/drive/trash' : action === 'copy' ? '/api/v1/drive/copy' : '/api/v1/drive/move', action === 'trash' ? { path: entry.path } : { from: entry.path, to: destination ? `${destination}/${entry.name}` : entry.name }, entry.version)
      }
      setSelected(new Set()); setError(null)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Bulk action stopped') }
    finally { await refresh().catch(() => {}); setBusy(false) }
  }

  async function restore(entry: TrashEntry, permanent: boolean) {
    if (permanent && !window.confirm(`Permanently delete /${entry.path}? This cannot be undone.`)) return
    setBusy(true)
    try {
      const response = await fetch(`/api/v1/drive/trash/${entry.id}${permanent ? '' : '/restore'}`, { method: permanent ? 'DELETE' : 'POST', headers: { 'X-CSRF-Token': csrfToken } })
      if (!response.ok) throw await responseError(response)
      await refresh(); setError(null)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Trash action failed') }
    finally { setBusy(false) }
  }

  async function uploadFiles() {
    if (uploads.length === 0) return
    setBusy(true); setError(null)
    try {
      for (const file of uploads) {
        if (storage && file.size > storage.maxFileBytes) throw new Error(`${file.name} exceeds the file size limit (${bytes(storage.maxFileBytes)}).`)
        setUploadName(file.name)
        const path = folder ? `${folder}/${file.name}` : file.name
        const response = await fetch('/api/v1/drive/uploads', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ path, size: file.size, fingerprint: await fingerprint(file) }) })
        if (!response.ok) throw await responseError(response)
        const session: { id: string; offset: number } = await response.json()
        let offset = session.offset
        setProgress(file.size ? offset / file.size : 1)
        while (offset < file.size) {
          const chunk = file.slice(offset, offset + chunkSize)
          const chunkHash = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', await chunk.arrayBuffer())), byte => byte.toString(16).padStart(2, '0')).join('')
          const result = await fetch(`/api/v1/drive/uploads/${session.id}`, { method: 'PATCH', headers: { 'X-CSRF-Token': csrfToken, 'X-Upload-Offset': String(offset), 'X-Chunk-SHA256': chunkHash }, body: chunk })
          if (!result.ok) throw await responseError(result)
          const next: { offset: number } = await result.json()
          if (next.offset <= offset || next.offset > file.size) throw new Error('Upload progress was invalid.')
          offset = next.offset
          setProgress(offset / file.size)
        }
        const complete = await fetch(`/api/v1/drive/uploads/${session.id}/complete`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken } })
        if (!complete.ok) throw await responseError(complete)
        setUploads((remaining) => remaining.slice(1))
        setProgress(null)
      }
      if (inputRef.current) inputRef.current.value = ''
      setUploadName('')
      await refresh()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Upload paused; select the same files to resume') }
    finally { setBusy(false) }
  }

  function onDrop(event: DragEvent<HTMLElement>) {
    event.preventDefault(); setDragActive(false)
    const files = Array.from(event.dataTransfer.files)
    if (files.length > 0) { setUploads(files); setNewMenu(false); setError(null) }
  }

  const previewURL = inspected ? `/api/v1/drive/preview?path=${encodeURIComponent(inspected.path)}` : ''
  const fileCount = visible.filter((entry) => entry.kind === 'file').length
  const folderCount = visible.length - fileCount

  return <section className="drive" aria-label="HomeDrive" onDragOver={(event) => { if (event.dataTransfer.types.includes('Files')) { event.preventDefault(); setDragActive(true) } }} onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node)) setDragActive(false) }} onDrop={onDrop}>
    {dragActive && <div className="drive-drop-overlay" aria-hidden="true">Drop files to upload</div>}
    <div className="drive-searchbar"><span aria-hidden="true">⌕</span><input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search files across HomeDrive" aria-label="Search files across HomeDrive" /></div>
    <div className="drive-toolbar">
      <div className="drive-toolbar-left">
        <div className="drive-new-wrap"><button className="drive-new-button" type="button" onClick={() => setNewMenu(!newMenu)}>＋ New</button>{newMenu && <div className="drive-popover"><button type="button" onClick={() => void createFolder()}>New folder</button><button type="button" onClick={() => { setNewMenu(false); inputRef.current?.click() }}>Upload files</button></div>}</div>
        <input ref={inputRef} className="sr-only" type="file" multiple aria-label="Choose files to upload" onChange={(event) => { setUploads(Array.from(event.target.files ?? [])); setError(null) }} />
        <button type="button" className={`drive-nav-button ${showTrash ? '' : 'active'}`} onClick={() => setShowTrash(false)}>All files</button>
        <button type="button" className={`drive-nav-button ${showTrash ? 'active' : ''}`} onClick={() => setShowTrash(true)}>Trash</button>
      </div>
      <div className="drive-toolbar-right">
        <label className="sr-only" htmlFor="drive-sort">Sort files</label><select id="drive-sort" value={sortBy} onChange={(event) => setSortBy(event.target.value as SortKey)}><option value="name">Name</option><option value="date">Modified</option><option value="size">Size</option><option value="type">Type</option></select>
        <button type="button" className="drive-icon-button" onClick={() => setDescending(!descending)} aria-label={descending ? 'Sort ascending' : 'Sort descending'} title={descending ? 'Sort ascending' : 'Sort descending'}>{descending ? '↓' : '↑'}</button>
        <button type="button" className={`drive-icon-button ${view === 'list' ? 'is-active' : ''}`} onClick={() => setView('list')} aria-label="List view" title="List view">☷</button>
        <button type="button" className={`drive-icon-button ${view === 'grid' ? 'is-active' : ''}`} onClick={() => setView('grid')} aria-label="Grid view" title="Grid view">▦</button>
      </div>
    </div>
    {!showTrash && <nav className="drive-breadcrumbs" aria-label="Folder path"><button type="button" onClick={() => navigate('')}>My Drive</button>{folder.split('/').filter(Boolean).map((part, index, parts) => <span key={index}> / <button type="button" onClick={() => navigate(parts.slice(0, index + 1).join('/'))}>{part}</button></span>)}</nav>}
    {storage && <div className="drive-storage"><span>{bytes(storage.usedBytes)} used of {bytes(storage.quotaBytes)}</span><progress max={storage.quotaBytes} value={Math.min(storage.usedBytes + storage.reservedBytes, storage.quotaBytes)} aria-label="Storage used" /><span>Max file {bytes(storage.maxFileBytes)}</span></div>}
    {uploads.length > 0 && <div className="drive-upload-tray"><span>{uploads.length} file{uploads.length === 1 ? '' : 's'} ready{uploadName ? ` · ${uploadName}` : ''}</span>{progress !== null && <progress max={1} value={progress} aria-label="Upload progress" />}<button type="button" disabled={busy} onClick={() => void uploadFiles()}>{busy ? 'Uploading…' : 'Upload / Resume'}</button>{!busy && <button type="button" className="drive-quiet-button" onClick={() => setUploads([])}>Clear</button>}</div>}
    {error && <p className="form-error" role="alert">{error}</p>}
    {showTrash ? <div className="drive-content"><div className="drive-section-title"><h2>Trash</h2><span>Deleted items are kept for 30 days by default</span></div>{trashEntries.length === 0 ? <p className="drive-empty">Trash is empty.</p> : <div className="drive-trash-list">{trashEntries.map((entry) => <div className="drive-trash-row" key={entry.id}><span className="drive-item-name">/{entry.path}</span><span className="drive-muted">{entry.deletedAt ? modified(entry.deletedAt) : ''}</span><button type="button" disabled={busy} onClick={() => void restore(entry, false)}>Restore</button><button type="button" disabled={busy} onClick={() => void restore(entry, true)}>Delete forever</button></div>)}</div>}</div> : <div className="drive-content">
      {!query && quickAccess.length > 0 && <section className="drive-quick" aria-label="Quick access"><h2>Quick access</h2><div className="drive-quick-grid">{quickAccess.map((entry) => <button type="button" className="drive-quick-item" key={entry.path} onClick={() => openEntry(entry)}><EntryIcon entry={entry} /><span><strong title={entry.name}>{entry.name}</strong><small>Updated {modified(entry.modifiedAt)}</small></span></button>)}</div></section>}
      <div className="drive-section-title"><h2>{query ? 'Search results' : folder ? folder.split('/').at(-1) : 'All files'}</h2><span>{visible.length} items</span></div>
      {selectedEntries.length > 0 && <div className="drive-selection-bar"><strong>{selectedEntries.length} selected</strong><button type="button" disabled={busy} onClick={() => void bulkAction('move')}>Move</button><button type="button" disabled={busy} onClick={() => void bulkAction('copy')}>Copy</button><button type="button" disabled={busy} onClick={() => void bulkAction('trash')}>Move to Trash</button><button type="button" className="drive-quiet-button" onClick={() => setSelected(new Set())}>Clear</button></div>}
      {loading || searching ? <p className="drive-empty">{searching ? 'Searching…' : 'Loading files…'}</p> : visible.length === 0 ? <p className="drive-empty">{query ? 'No matching files or folders.' : 'This folder is empty. Create a folder or drop files here.'}</p> : view === 'list' ? <div className="drive-table"><div className="drive-table-head"><input type="checkbox" aria-label="Select all visible items" checked={visible.length > 0 && visible.every((entry) => selected.has(entry.path))} onChange={() => setSelected(visible.every((entry) => selected.has(entry.path)) ? new Set() : new Set(visible.map((entry) => entry.path)))} /><span>Name</span><span>Size</span><span>Modified</span><span className="sr-only">Actions</span></div><ul className="drive-table-body">{visible.map((entry) => <li className={`drive-data-row ${selected.has(entry.path) ? 'selected' : ''}`} key={entry.path}><input type="checkbox" aria-label={`Select ${entry.name}`} checked={selected.has(entry.path)} onChange={() => toggleSelected(entry.path)} /><button type="button" className="drive-name-button" onClick={() => openEntry(entry)}><EntryIcon entry={entry} /><span title={entry.path}>{entry.name}</span></button><span className="drive-muted">{entry.kind === 'folder' ? '—' : bytes(entry.size)}</span><span className="drive-muted">{modified(entry.modifiedAt)}</span><div className="drive-row-menu"><button type="button" className="drive-icon-button" aria-label={`Actions for ${entry.name}`} onClick={() => setOpenMenu(openMenu === entry.path ? null : entry.path)}>⋯</button>{openMenu === entry.path && <div className="drive-popover drive-action-menu"><button type="button" onClick={() => { setInspected(entry); setOpenMenu(null) }}>Details{previewable.test(entry.name) ? ' / Preview' : ''}</button><button type="button" disabled={busy} onClick={() => void changeEntry(entry, 'rename')}>Rename</button><button type="button" disabled={busy} onClick={() => void changeEntry(entry, 'move')}>Move</button><button type="button" disabled={busy} onClick={() => void changeEntry(entry, 'copy')}>Copy</button><a href={entry.kind === 'folder' ? `/api/v1/drive/folders/zip?path=${encodeURIComponent(entry.path)}` : `/api/v1/drive/files?path=${encodeURIComponent(entry.path)}`}>{entry.kind === 'folder' ? 'Download ZIP' : 'Download'}</a><button type="button" disabled={busy} onClick={() => void changeEntry(entry, 'trash')}>Move to Trash</button></div>}</div></li>)}</ul></div> : <ul className="drive-grid">{visible.map((entry) => <li className={selected.has(entry.path) ? 'selected' : ''} key={entry.path}><input type="checkbox" aria-label={`Select ${entry.name}`} checked={selected.has(entry.path)} onChange={() => toggleSelected(entry.path)} /><button type="button" className="drive-grid-open" onClick={() => openEntry(entry)}><EntryIcon entry={entry} /><strong title={entry.path}>{entry.name}</strong><small>{entry.kind === 'folder' ? 'Folder' : bytes(entry.size)} · {modified(entry.modifiedAt)}</small></button><button type="button" className="drive-icon-button" aria-label={`Details for ${entry.name}`} onClick={() => setInspected(entry)}>ⓘ</button></li>)}</ul>}
      <div className="drive-footer"><span>{fileCount} files · {folderCount} folders</span><span>{bytes(visible.filter((entry) => entry.kind === 'file').reduce((sum, entry) => sum + entry.size, 0))}</span></div>
    </div>}
    {inspected && <div className="drive-inspector-backdrop" onClick={() => setInspected(null)}><aside className="drive-inspector" onClick={(event) => event.stopPropagation()} aria-label="File details"><div className="drive-inspector-head"><h2>{inspected.name}</h2><button type="button" className="drive-icon-button" aria-label="Close details" onClick={() => setInspected(null)}>×</button></div><div className="drive-inspector-preview">{imageFile.test(inspected.name) ? <img src={previewURL} alt={inspected.name} /> : /\.pdf$/i.test(inspected.name) ? <iframe title={`Preview ${inspected.name}`} src={previewURL} /> : textFile.test(inspected.name) ? <pre>{previewText ?? 'Loading preview…'}</pre> : <EntryIcon entry={inspected} />}</div><dl><dt>Location</dt><dd>/{inspected.path}</dd><dt>Type</dt><dd>{fileType(inspected)}</dd><dt>Size</dt><dd>{inspected.kind === 'folder' ? 'Folder' : bytes(inspected.size)}</dd><dt>Modified</dt><dd>{new Date(inspected.modifiedAt).toLocaleString()}</dd></dl>{inspected.kind === 'file' && <a className="drive-download" href={`/api/v1/drive/files?path=${encodeURIComponent(inspected.path)}`}>Download</a>}</aside></div>}
  </section>
}
