import { useCallback, useEffect, useState, type FormEvent } from 'react'

type Entry = { name: string; path: string; kind: 'file' | 'folder'; size: number; modifiedAt: string }

type Props = { csrfToken: string }

export default function Drive({ csrfToken }: Props) {
  const [folder, setFolder] = useState('')
  const [entries, setEntries] = useState<Entry[]>([])
  const [name, setName] = useState('')
  const [upload, setUpload] = useState<File | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

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
    try {
      const path = folder ? `${folder}/${upload.name}` : upload.name
      const response = await fetch(`/api/v1/drive/files?path=${encodeURIComponent(path)}`, {
        method: 'PUT', headers: { 'X-CSRF-Token': csrfToken }, body: upload,
      })
      if (!response.ok) throw new Error(response.status === 409 ? 'A file with that name already exists' : 'Upload failed. Check the available disk space and try again.')
      setUpload(null)
      const input = document.getElementById('drive-upload') as HTMLInputElement | null
      if (input) input.value = ''
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Upload failed') }
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
      <div><label htmlFor="drive-upload">Upload a file</label><div className="drive-action-row"><input id="drive-upload" type="file" onChange={(event) => setUpload(event.target.files?.[0] ?? null)} /><button type="button" disabled={!upload || busy} onClick={() => void uploadFile()}>{busy ? 'Working…' : 'Upload'}</button></div></div>
    </div>
    <p className="drive-note">No fixed per-file cap. Uploads need enough free disk space and must complete in one session.</p>
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
