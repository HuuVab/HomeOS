import { useCallback, useEffect, useState, type FormEvent } from 'react'

type Entry = { name: string; path: string; kind: 'file' | 'folder'; size: number; modifiedAt: string }

type Props = { csrfToken: string }
type UploadSession = { id: string; offset: number; size: number }
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
  const [upload, setUpload] = useState<File | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [progress, setProgress] = useState<number | null>(null)

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
    if (!upload) return
    setBusy(true)
    setError(null)
    setProgress(0)
    try {
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
      setUpload(null)
      setProgress(null)
      const input = document.getElementById('drive-upload') as HTMLInputElement | null
      if (input) input.value = ''
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Upload paused. Select the same file and try again.') }
    finally { setBusy(false) }
  }

  return <section className="drive" aria-label="HomeDrive">
    <div className="section-heading"><div><p className="eyebrow">Your storage</p><h2>Files and folders</h2></div></div>
    <p className="drive-path">Location: /{folder}</p>
    <div className="drive-actions">
      <form onSubmit={(event) => void createFolder(event)}>
        <label htmlFor="folder-name">New folder</label>
        <div className="drive-action-row"><input id="folder-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="Folder name" /><button disabled={busy}>Create</button></div>
      </form>
      <div><label htmlFor="drive-upload">Upload a file</label><div className="drive-action-row"><input id="drive-upload" type="file" disabled={busy} onChange={(event) => { setUpload(event.target.files?.[0] ?? null); setProgress(null); setError(null) }} /><button type="button" disabled={!upload || busy} onClick={() => void uploadFile()}>{busy ? 'Uploading…' : progress !== null ? 'Resume' : 'Upload'}</button></div>
        {upload && progress !== null && <div className="drive-progress"><progress max={upload.size || 1} value={progress} aria-label="Upload progress" /><span>{upload.size ? Math.round(progress / upload.size * 100) : 100}% · {Math.round(progress / 1024 / 1024)} of {Math.round(upload.size / 1024 / 1024)} MiB</span></div>}</div>
    </div>
    <p className="drive-note">No fixed per-file cap. Uploads resume after interruption when you select the same file again; unfinished files stay hidden until complete.</p>
    {error && <p className="form-error" role="alert">{error}</p>}
    {folder && <button type="button" className="secondary-button" onClick={() => setFolder(folder.split('/').slice(0, -1).join('/'))}>↑ Parent folder</button>}
    {loading ? <p className="empty">Loading files…</p> : entries.length === 0 ? <p className="empty">This folder is empty.</p> :
      <ul className="drive-list">{entries.map((entry) => <li key={entry.path}>
        <span><span className="entry-kind">{entry.kind === 'folder' ? 'Folder' : 'File'}</span>{entry.name}</span>
        {entry.kind === 'folder' ? <button type="button" className="secondary-button" onClick={() => setFolder(entry.path)}>Open</button> :
          <a className="secondary-button download-link" href={`/api/v1/drive/files?path=${encodeURIComponent(entry.path)}`}>Download</a>}
      </li>)}</ul>}
  </section>
}
