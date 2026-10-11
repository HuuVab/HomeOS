# HomeDrive — Feature Implementation Roadmap

**Project:** HomeDrive  
**Concept:** A self-hosted Google Drive alternative built from scratch  
**Integration:** Standalone service within HomeOS  
**Goal:** Lightweight, secure, multi-user cloud storage with web access, desktop sync, sharing, and extensibility.

## Priority definitions

- **P0 — MVP:** Essential for a working private drive.
- **P1 — Core:** A practical Google Drive-like experience.
- **P2 — Advanced:** Power-user, integration, and collaboration capabilities.
- **P3 — Future:** AI, enterprise, and ambitious enhancements.

## 1. File management

- [x] **P0** Upload single files
- [x] **P0** Upload multiple files
- [ ] **P0** Drag-and-drop uploads
- [x] **P0** Create folders and nested folders
- [x] **P0** Rename files and folders
- [x] **P0** Move files and folders
- [x] **P0** Copy files and folders
- [x] **P0** Delete files and folders
- [x] **P0** Download files
- [x] **P0** Download folders as ZIP
- [ ] **P0** File list and grid views
- [ ] **P0** Sort by name, date, size, and type
- [x] **P0** File details and metadata
- [ ] **P1** Bulk selection and operations
- [ ] **P1** Cut, copy, and paste
- [ ] **P1** Starred/favorite files
- [ ] **P1** Recent files
- [ ] **P1** File type filters
- [ ] **P1** Keyboard shortcuts
- [ ] **P1** Breadcrumb navigation
- [ ] **P1** File version history and restore
- [x] **P1** Recycle bin, restore, and permanent deletion
- [x] **P1** Automatically delete Trash items after a configurable retention period (default 30 days)
- [ ] **P1** Duplicate file handling
- [ ] **P1** File shortcuts (references without duplicating content)
- [ ] **P2** Custom tags and labels
- [ ] **P2** File locking
- [ ] **P2** Archive extraction
- [ ] **P2** File templates
- [ ] **P2** Custom metadata fields

## 2. Upload and transfer engine

- [x] **P0** Per-file upload progress
- [x] **P0** Upload error handling
- [ ] **P0** File size limits and filename validation
- [x] **P1** Chunked and resumable uploads
- [ ] **P1** Pause and resume uploads
- [ ] **P1** Parallel uploads and upload queue
- [ ] **P1** Automatic retry of failed transfers
- [ ] **P1** Upload speed indicator
- [ ] **P1** Duplicate upload detection
- [x] **P1** Streaming large-file downloads
- [ ] **P1** Integrity checks and checksums
- [ ] **P2** Transfer bandwidth throttling
- [ ] **P2** Content-addressable deduplication
- [ ] **P2** Background transfer processing

## 3. File previews and media

- [ ] **P0** Image previews
- [ ] **P0** PDF previews
- [ ] **P1** Video streaming with seeking
- [ ] **P1** Audio playback
- [ ] **P1** Plain-text, Markdown, and code previews
- [ ] **P1** Syntax highlighting
- [ ] **P1** DOCX, XLSX, and PPTX previews
- [ ] **P1** Thumbnail generation
- [ ] **P1** Image gallery and slideshow
- [ ] **P2** EXIF metadata viewer
- [ ] **P2** Video thumbnails
- [ ] **P2** Subtitle support
- [ ] **P2** Image rotation and editing
- [ ] **P2** Media transcoding
- [ ] **P3** RAW photo and 3D model previews

## 4. Search and organization

- [ ] **P0** Search by filename
- [ ] **P1** Search by folder, file type, date, and size
- [ ] **P1** Search in trash and shared files
- [ ] **P1** Recent search history
- [ ] **P2** Full-text document search
- [ ] **P2** Advanced search operators
- [ ] **P2** Saved searches
- [ ] **P2** OCR-searchable PDFs
- [ ] **P2** Search by tags
- [ ] **P3** Semantic and natural-language search

## 5. Sharing and permissions

- [ ] **P0** Private user files and ownership
- [ ] **P0** Server-enforced authorization for every file operation
- [ ] **P1** Share with specific users
- [ ] **P1** Public sharing links
- [ ] **P1** Read-only and editable shares
- [ ] **P1** Password-protected links
- [ ] **P1** Expiring links
- [ ] **P1** Revoke sharing
- [ ] **P1** Shared with me / Shared by me
- [ ] **P1** Folder permission inheritance
- [ ] **P1** Per-file capabilities (allowed actions for current user)
- [ ] **P2** Upload-only file requests
- [ ] **P2** Team shared folders / shared drives
- [ ] **P2** User groups and group permissions
- [ ] **P2** Share activity history
- [ ] **P2** Guest accounts
- [ ] **P2** Download restrictions and maximum link uses
- [ ] **P3** Domain-restricted and approval-based sharing

## 6. Accounts and authentication

- [ ] **P0** Registration or administrator invitations
- [ ] **P0** Login and logout
- [x] **P0** Secure password hashing
- [x] **P0** Secure sessions
- [ ] **P0** User profile
- [ ] **P0** Account isolation
- [ ] **P1** Password reset and change
- [ ] **P1** Multi-factor authentication
- [ ] **P1** Session and device management
- [ ] **P1** Per-user storage quotas
- [ ] **P1** Administrator accounts
- [ ] **P2** Passkeys
- [ ] **P2** OAuth / SSO
- [ ] **P2** User group management
- [ ] **P2** Account deactivation, export, and deletion
- [ ] **P3** LDAP and enterprise identity federation

## 7. Synchronization and backup

- [ ] **P1** Desktop synchronization client
- [ ] **P1** Selective folder synchronization
- [ ] **P1** Two-way synchronization
- [ ] **P1** Sync status indicators
- [ ] **P1** Conflict detection and resolution
- [ ] **P1** Deletion propagation with recovery
- [ ] **P1** Offline file access
- [ ] **P1** Change feed API with cursors for incremental sync
- [ ] **P1** Revision IDs for conflict-safe updates
- [ ] **P2** Windows Explorer integration
- [ ] **P2** macOS Finder and Linux integration
- [ ] **P2** On-demand virtual files
- [ ] **P2** Automatic camera uploads
- [ ] **P2** Background synchronization
- [ ] **P2** Device-specific sync settings
- [ ] **P2** File change notifications
- [ ] **P2** LAN-optimized transfers
- [ ] **P2** Server and database backup/restore
- [ ] **P3** Incremental block-level sync
- [ ] **P3** End-to-end encrypted synchronization

## 8. Security and reliability

- [ ] **P0** HTTPS support
- [ ] **P0** Authorization on every API endpoint
- [x] **P0** Path traversal protection
- [ ] **P0** Secure internal file paths
- [ ] **P0** Database transactions for metadata
- [ ] **P0** Atomic upload commit
- [ ] **P0** File size and quota enforcement
- [ ] **P0** Rate limiting
- [ ] **P0** Safe file download headers
- [x] **P0** CSRF protection where applicable
- [ ] **P0** Upload validation
- [ ] **P1** Audit logs and admin activity logs
- [ ] **P1** Malware scanning integration
- [ ] **P1** Checksum verification
- [ ] **P1** Encrypted backups
- [ ] **P1** Secure share tokens
- [ ] **P2** Encryption at rest
- [ ] **P2** Retention policies
- [ ] **P2** Ransomware recovery workflows
- [ ] **P2** Storage integrity scans
- [ ] **P2** Disaster recovery testing
- [ ] **P3** Client-side end-to-end encryption

## 9. Administration and storage

- [ ] **P0** Storage capacity dashboard
- [ ] **P0** Used/free storage display
- [ ] **P0** User list
- [ ] **P1** Per-user storage usage and quotas
- [ ] **P1** Storage breakdown by file type
- [ ] **P1** Admin settings
- [ ] **P1** Background job status
- [ ] **P1** System health checks
- [ ] **P1** Error logs
- [ ] **P2** Multiple storage volumes
- [ ] **P2** External SMB/NFS storage
- [ ] **P2** S3-compatible storage
- [ ] **P2** Disk health integration
- [ ] **P2** Retention and cleanup jobs
- [ ] **P2** Prometheus metrics
- [ ] **P2** Storage migration tools
- [ ] **P3** Multi-node replication

## 10. User interface and experience

- [x] **P0** Responsive web dashboard
- [x] **P0** Sidebar navigation
- [ ] **P0** File table and context menus
- [ ] **P0** Upload drop zone
- [x] **P0** Loading, error, and empty states
- [ ] **P1** Dark/light theme
- [ ] **P1** Grid/list toggle
- [ ] **P1** Right-click actions
- [ ] **P1** Keyboard navigation and shortcuts
- [ ] **P1** Toast notifications
- [ ] **P1** Multi-select with Shift/Ctrl
- [ ] **P1** Storage usage indicator
- [ ] **P1** File details side panel
- [ ] **P1** Mobile browser support
- [ ] **P2** Customizable dashboard
- [ ] **P2** Multilingual UI
- [ ] **P2** Accessibility enhancements
- [ ] **P2** Installable PWA and offline shell
- [ ] **P3** Native mobile applications

## 11. Collaboration and productivity

- [ ] **P2** File comments and @mentions
- [ ] **P2** Activity feed and notifications
- [ ] **P2** Shared workspace folders
- [ ] **P2** Markdown and plain-text editor
- [ ] **P2** Office document, spreadsheet, and presentation editor integration
- [ ] **P3** Real-time collaborative editing
- [ ] **P3** Suggestions and change tracking
- [ ] **P3** Document approval workflows
- [ ] **P3** Electronic signatures

## 12. API and HomeOS integration

- [x] **P0** Versioned REST API
- [ ] **P0** File/folder CRUD endpoints
- [x] **P0** Authentication API
- [x] **P0** Health endpoint
- [ ] **P1** Sharing API
- [ ] **P1** Search API
- [x] **P1** Upload session API
- [ ] **P1** File events and notifications
- [ ] **P1** OpenAPI documentation
- [ ] **P1** Docker deployment (configured, runtime not verified on target host)
- [x] **P1** Persistent volume configuration
- [ ] **P2** WebDAV server
- [ ] **P2** Webhooks
- [ ] **P2** Scoped service API tokens
- [ ] **P2** HomeOS SSO integration
- [ ] **P2** Background worker queue
- [ ] **P2** CLI management tools
- [ ] **P2** Extension/plugin system
- [ ] **P3** Third-party SDK
- [ ] **P3** Federated sharing between servers

## 13. AI and intelligent features

- [ ] **P3** Automatic file categorization
- [ ] **P3** AI file summaries
- [ ] **P3** OCR extraction
- [ ] **P3** Image object recognition
- [ ] **P3** Duplicate and similar image detection
- [ ] **P3** Semantic document search
- [ ] **P3** Ask questions about documents
- [ ] **P3** Automatic tagging
- [ ] **P3** Local AI model integration
- [ ] **P3** AI-assisted file organization

---

## 14. Google Drive behavior details

| Feature | Priority | Implementation requirement |
|---|---|---|
| File shortcuts | P1 | Point to an existing file without copying bytes |
| File ownership | P0 | Persist owner ID and enforce access checks |
| Folder permission inheritance | P1 | Define and enforce inherited permissions |
| Shared drives | P2 | Team-owned storage distinct from personal storage |
| Change feed | P1 | Cursor-based incremental change API for sync clients |
| File revision IDs | P1 | Immutable version identifiers for restores and conflicts |
| Per-user visibility | P0 | Filter all queries to authorized resources |
| Last opened timestamp | P1 | Record access history per user |
| File capabilities | P1 | API returns operations the user is authorized to perform |
| Checksums | P1 | Validate file integrity on upload and download |
| Stream vs mirror | P2 | Support cloud-only and full-local-copy sync modes |

## 15. Development phases

### Phase 1 — Working cloud drive (P0)

Build authentication, account isolation, basic upload/download, folders, rename, move, copy, delete, trash, basic filename search, storage quotas, secure storage, and a responsive UI.

**Exit criteria:** Multiple users can securely manage private files in a browser, with persistent storage across Docker restarts.

### Phase 2 — Google Drive-like core (P1)

Add sharing, permission inheritance, file versions, thumbnails, resumable uploads, advanced search, activity history, and a desktop sync client.

**Exit criteria:** Users can share files safely and synchronize selected folders between devices with conflict handling.

### Phase 3 — Advanced HomeDrive (P2)

Add WebDAV, office editing integrations, external storage, team drives, mobile camera backup, observability, and HomeOS SSO.

**Exit criteria:** HomeDrive works as an extensible storage service in the broader HomeOS ecosystem.

### Phase 4 — Intelligent HomeDrive (P3)

Add local AI search, automatic organization, document Q&A, image recognition, collaborative editing, and plugins.

**Exit criteria:** HomeDrive offers advanced self-hosted productivity features beyond basic file storage.

## 16. Recommended technical stack

| Layer | Technology | Notes |
|---|---|---|
| Frontend | React + TypeScript | Responsive SPA |
| Backend | Go | Lightweight and efficient |
| HTTP router | Chi or Gin | Versioned REST API |
| Database | PostgreSQL | Users, files, permissions, versions, shares |
| File content storage | Local filesystem | Abstract behind a storage interface |
| Future object storage | S3-compatible backend | Optional expansion |
| Authentication | Secure sessions or short-lived tokens | Support future SSO |
| Background processing | Go workers | Previews, cleanup, scans |
| Cache/queue | Redis (when needed) | Avoid premature complexity |
| Search | PostgreSQL initially | Extend to full-text/semantic search |
| Real-time events | SSE or WebSockets | Progress and file changes |
| Deployment | Docker Compose | Windows Docker homelab |
| Reverse proxy | Caddy | HTTPS and routing |
| Desktop sync client | Go | Reuse API and change feed |

## 17. Architecture principles

1. **Separate file bytes from metadata:** Store contents in a storage backend and metadata in PostgreSQL.
2. **Use stable internal IDs:** UUIDs identify resources; user-facing filenames are not physical storage keys.
3. **Enforce authorization server-side:** Never trust client-visible paths, permissions, or ownership claims.
4. **Use atomic writes:** Stage uploads, verify checksums, and commit metadata/content consistently.
5. **Make sync explicit:** Use revisions and change cursors, and define conflict and deletion semantics.
6. **Keep storage pluggable:** Start with local disk, then support S3/NAS backends.
7. **Treat backups separately from sync:** Maintain restorable backups of both PostgreSQL and file contents.
8. **Keep HomeDrive independent:** Separate its storage and database from Nextcloud, while exposing APIs for HomeOS.
9. **Design for Docker persistence:** Use persistent volumes, health checks, migrations, and restart policies.
10. **Prioritize correctness and security:** Complete P0/P1 before adding AI or advanced collaboration.

## 18. Initial suggested backend entities

- **users:** ID, identity, credentials reference, role, quota, status.
- **files:** ID, owner, parent folder, name, MIME type, size, current revision, timestamps, trash state.
- **file_versions:** revision ID, file ID, storage key, checksum, size, creator, timestamp.
- **shares:** ID, target resource, grantor, grantee or token, permission, expiry.
- **upload_sessions:** ID, owner, target, expected size, received chunks, expiry, state.
- **change_events:** sequence/cursor, resource ID, event type, actor, timestamp.
- **audit_events:** actor, operation, target, result, timestamp.
- **devices:** user ID, device ID, sync cursor, last seen.

**Recommended starting scope:** Complete Phase 1, then Phase 2. This produces a useful Google Drive alternative before tackling expensive extras such as collaborative editing and AI.
