import { useCallback, useEffect, useState, type FormEvent } from 'react'

type Entry = { name: string; path: string; kind: 'file' | 'folder'; size: number; modifiedAt: string }

type Props = { csrfToken: string }
type UploadSession = { id: string; offset: number; size: number }
type TrashEntry = { id: string; path: string; deletedAt: string }
const chunkSize = 4 * 1024 * 1024

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

export default function Drive({ csrfToken }: Props) {
  const [folder, setFolder] = useState('')
  const [entries, setEntries] = useState<Entry[]>([])
  const [name, setName] = useState('')
  const [uploads, setUploads] = useState<File[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [progress, setProgress] = useState<number | null>(null)
  const [query, setQuery] = useState('')
  const [searchEntries, setSearchEntries] = useState<Entry[]>([])
  const [searching, setSearching] = useState(false)
  const [preview, setPreview] = useState<Entry | null>(null)
  const [sortBy, setSortBy] = useState<'name' | 'date' | 'size' | 'type'>('name')
  const [descending, setDescending] = useState(false)
  const [details, setDetails] = useState<string | null>(null)
  const [showTrash, setShowTrash] = useState(false)
  const [trashEntries, setTrashEntries] = useState<TrashEntry[]>([])
  const visibleEntries = (query.trim() ? searchEntries : entries).slice().sort((a, b) => {
    const comparison = sortBy === 'date' ? a.modifiedAt.localeCompare(b.modifiedAt)
      : sortBy === 'size' ? a.size - b.size
      : sortBy === 'type' ? a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name)
      : a.name.localeCompare(b.name)
    return descending ? -comparison : comparison
  })

  useEffect(() => {
    if (!query.trim()) { setSearchEntries([]); setSearching(false); return }
    const controller = new AbortController()
    setSearching(true)
    const timer = window.setTimeout(async () => {
      try {
        const response = await fetch(`/api/v1/drive/search?q=${encodeURIComponent(query.trim())}`, { signal: controller.signal, cache: 'no-store' })
        if (!response.ok) throw new Error('Search failed')
        const data: { entries: Entry[] } = await response.json()
        setSearchEntries(data.entries)
      } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Search failed') }
      finally { if (!controller.signal.aborted) setSearching(false) }
    }, 250)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [query])

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch(`/api/v1/drive?path=${encodeURIComponent(folder)}`, { cache: 'no-store', signal })
      if (!response.ok) throw new Error('Unable to load files')
      const data: { entries: Entry[] } = await response.json()
      setEntries(data.entries)
      setError(null)
    } catch (cause) {
      if (!signal?.aborted) setError(cause instanceof Error ? cause.message : 'Unable to load files')
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [folder])

  const loadTrash = useCallback(async () => {
    const response = await fetch('/api/v1/drive/trash', { cache: 'no-store' })
    if (!response.ok) throw new Error('Unable to load Trash')
    const data: { entries: TrashEntry[] } = await response.json()
    setTrashEntries(data.entries)
  }, [])

  useEffect(() => { if (showTrash) void loadTrash().catch((cause) => setError(cause instanceof Error ? cause.message : 'Unable to load Trash')) }, [showTrash, loadTrash])

  async function changeEntry(entry: Entry, action: 'rename' | 'move' | 'copy' | 'trash') {
    let endpoint = '/api/v1/drive/trash'
    let payload: object = { path: entry.path }
    if (action !== 'trash') {
      const proposed = window.prompt(action === 'rename' ? 'New name' : 'Destination path from drive root', action === 'rename' ? entry.name : action === 'copy' ? `${entry.path}-copy` : entry.path)
      if (proposed === null) return
      const to = action === 'rename' ? [...entry.path.split('/').slice(0, -1), proposed].filter(Boolean).join('/') : proposed
      endpoint = action === 'copy' ? '/api/v1/drive/copy' : '/api/v1/drive/move'
      payload = { from: entry.path, to }
    } else if (!window.confirm(`Move ${entry.name} to Trash?`)) return
    setBusy(true)
    try {
      const response = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(payload) })
      if (!response.ok) throw new Error(response.status === 409 ? 'Destination already exists' : 'Could not change this item')
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not change this item') }
    finally { setBusy(false) }
  }

  async function restoreEntry(entry: TrashEntry) {
    setBusy(true)
    try {
      const response = await fetch(`/api/v1/drive/trash/${entry.id}/restore`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken } })
      if (!response.ok) throw new Error(response.status === 409 ? 'Original location is occupied' : 'Could not restore item')
      await Promise.all([loadTrash(), load()])
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not restore item') }
    finally { setBusy(false) }
  }

  async function deleteForever(entry: TrashEntry) {
    if (!window.confirm(`Permanently delete /${entry.path}? This cannot be undone.`)) return
    setBusy(true)
    try {
      const response = await fetch(`/api/v1/drive/trash/${entry.id}`, { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } })
      if (!response.ok) throw new Error('Could not permanently delete item')
      await loadTrash()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not permanently delete item') }
    finally { setBusy(false) }
  }

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  async function createFolder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!name.trim()) return
    setBusy(true)
    try {
      const response = await fetch('/api/v1/drive/folders', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ path: folder ? `${folder}/${name.trim()}` : name.trim() }),
      })
      if (!response.ok) throw new Error(response.status === 409 ? 'A file or folder with that name already exists' : 'Could not create folder')
      setName('')
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not create folder') }
    finally { setBusy(false) }
  }

  async function uploadFile() {
    if (uploads.length === 0) return
    setBusy(true)
    setError(null)
    setProgress(0)
    try {
      for (const upload of uploads) {
      const path = folder ? `${folder}/${upload.name}` : upload.name
      const response = await fetch('/api/v1/drive/uploads', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ path, size: upload.size, fingerprint: await fingerprint(upload) }),
      })
      if (!response.ok) throw new Error(response.status === 409 ? 'A file with that name already exists, or an unfinished upload has different contents.' : 'Could not start upload.')
      const session: UploadSession = await response.json()
      let offset = session.offset
      setProgress(offset)
      while (offset < upload.size) {
        const chunk = upload.slice(offset, offset + chunkSize)
        const result = await fetch(`/api/v1/drive/uploads/${session.id}`, {
          method: 'PATCH', headers: { 'X-CSRF-Token': csrfToken, 'X-Upload-Offset': String(offset) }, body: chunk,
        })
        if (!result.ok) throw new Error('Upload paused. Select the same file and click Resume to continue.')
        const next: { offset: number } = await result.json()
        if (next.offset <= offset || next.offset > upload.size) throw new Error('Upload progress was invalid. Try resuming.')
        offset = next.offset
        setProgress(offset)
      }
      const complete = await fetch(`/api/v1/drive/uploads/${session.id}/complete`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken } })
      if (!complete.ok) throw new Error('Upload verification failed. Select the same file and try again.')
      }
      setUploads([])
      setProgress(null)
      const input = document.getElementById('drive-upload') as HTMLInputElement | null
      if (input) input.value = ''
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Upload paused. Select the same file and try again.') }
    finally { setBusy(false) }
  }

  return <section className="drive" aria-label="HomeDrive">
    <div className="section-heading"><div><p className="eyebrow">Your storage</p><h2>Files and folders</h2></div></div>
    <nav className="drive-path" aria-label="Folder path"><button type="button" className="secondary-button" onClick={() => setFolder('')}>Root</button>{folder.split('/').filter(Boolean).map((part, index, parts) => <span key={index}> / <button type="button" className="secondary-button" onClick={() => setFolder(parts.slice(0, index + 1).join('/'))}>{part}</button></span>)}</nav>
    <div className="drive-actions">
      <form onSubmit={(event) => void createFolder(event)}>
        <label htmlFor="folder-name">New folder</label>
        <div className="drive-action-row"><input id="folder-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="Folder name" /><button disabled={busy}>Create</button></div>
      </form>
      <div><label htmlFor="drive-upload">Upload files</label><div className="drive-action-row"><input id="drive-upload" type="file" multiple disabled={busy} onChange={(event) => { setUploads(Array.from(event.target.files ?? [])); setProgress(null); setError(null) }} /><button type="button" disabled={uploads.length === 0 || busy} onClick={() => void uploadFile()}>{busy ? 'Uploading…' : progress !== null ? 'Resume' : 'Upload'}</button></div>
        {uploads.length > 0 && <p className="drive-note">{uploads.length} file(s) selected</p>}{uploads.length > 0 && progress !== null && <div className="drive-progress"><progress max={uploads[0].size || 1} value={progress} aria-label="Upload progress" /><span>{uploads[0].size ? Math.round(progress / uploads[0].size * 100) : 100}%</span></div>}</div>
    </div>
    <p className="drive-note">No fixed per-file cap. Uploads resume after interruption when you select the same file again; unfinished files stay hidden until complete.</p>
    {error && <p className="form-error" role="alert">{error}</p>}
    <button type="button" className="secondary-button" onClick={() => setShowTrash(!showTrash)}>{showTrash ? 'Show files' : 'Trash'}</button>
    {showTrash ? <><p className="drive-note">Items are automatically deleted after 30 days unless the server retention setting is changed.</p><ul className="drive-list">{trashEntries.map((entry) => <li key={entry.id}><span>/{entry.path}</span><button type="button" disabled={busy} onClick={() => void restoreEntry(entry)}>Restore</button><button type="button" disabled={busy} onClick={() => void deleteForever(entry)}>Delete forever</button></li>)}{trashEntries.length === 0 && <li>Trash is empty.</li>}</ul></> : <>
    <div className="drive-actions">
      <div><label htmlFor="drive-search">Search all folders</label><input id="drive-search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search filenames" /></div>
      <div><label htmlFor="drive-sort">Sort by</label><div className="drive-action-row"><select id="drive-sort" value={sortBy} onChange={(event) => setSortBy(event.target.value as typeof sortBy)}><option value="name">Name</option><option value="date">Modified date</option><option value="size">Size</option><option value="type">Type</option></select><button type="button" className="secondary-button" onClick={() => setDescending(!descending)} aria-label={descending ? 'Sort ascending' : 'Sort descending'}>{descending ? 'Descending' : 'Ascending'}</button></div></div>
    </div>
    {folder && <button type="button" className="secondary-button" onClick={() => setFolder(folder.split('/').slice(0, -1).join('/'))}>↑ Parent folder</button>}
    {loading || searching ? <p className="empty">{searching ? 'Searching…' : 'Loading files…'}</p> : visibleEntries.length === 0 ? <p className="empty">{query ? 'No matching files or folders.' : 'This folder is empty.'}</p> :
      <ul className="drive-list">{visibleEntries.map((entry) => <li key={entry.path}>
        <span><span className="entry-kind">{entry.kind === 'folder' ? 'Folder' : 'File'}</span>{entry.name}</span>
        <button type="button" className="secondary-button" onClick={() => setDetails(details === entry.path ? null : entry.path)}>{details === entry.path ? 'Hide details' : 'Details'}</button>
        <button type="button" className="secondary-button" disabled={busy} onClick={() => void changeEntry(entry, 'rename')}>Rename</button>
        <button type="button" className="secondary-button" disabled={busy} onClick={() => void changeEntry(entry, 'move')}>Move</button>
        <button type="button" className="secondary-button" disabled={busy} onClick={() => void changeEntry(entry, 'copy')}>Copy</button>
        <button type="button" className="secondary-button" disabled={busy} onClick={() => void changeEntry(entry, 'trash')}>Delete</button>
        {entry.kind === 'folder' ? <><button type="button" className="secondary-button" onClick={() => setFolder(entry.path)}>Open</button><a className="secondary-button download-link" href={`/api/v1/drive/folders/zip?path=${encodeURIComponent(entry.path)}`}>ZIP</a></> :
          <a className="secondary-button download-link" href={`/api/v1/drive/files?path=${encodeURIComponent(entry.path)}`}>Download</a>}
        {entry.kind === 'file' && /\.(png|jpe?g|gif|webp|pdf)$/i.test(entry.name) && <button type="button" className="secondary-button" onClick={() => setPreview(entry)}>Preview</button>}
        {details === entry.path && <div className="drive-note">Path: /{entry.path} · Size: {entry.size.toLocaleString()} bytes · Modified: {new Date(entry.modifiedAt).toLocaleString()}</div>}
      </li>)}</ul>}
    {preview && <div className="drive-preview"><button type="button" onClick={() => setPreview(null)}>Close preview</button>{/\.pdf$/i.test(preview.name) ? <iframe title={`Preview ${preview.name}`} src={`/api/v1/drive/preview?path=${encodeURIComponent(preview.path)}`} /> : <img alt={`Preview ${preview.name}`} src={`/api/v1/drive/preview?path=${encodeURIComponent(preview.path)}`} />}</div>}
    </>}
  </section>
}
