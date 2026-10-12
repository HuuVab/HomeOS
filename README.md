# HomeOS

HomeOS is a planned self-hosted personal cloud and homelab management platform. It aims to provide one modular web interface for files, media, monitoring, containers, backups, automation, and related services.

The proposed foundation is:

- Go modular-monolith backend
- React and TypeScript frontend
- PostgreSQL authoritative server metadata (SQLite is available for standalone development)
- Shared filesystem storage
- Docker Compose deployment

## Project status

HomeOS has a loopback-only Go API, PostgreSQL-backed server metadata, a React dashboard with native Windows host metrics, and Docker Compose packaging. SQLite remains an explicit standalone-development option, not a second server source of truth. The first HomeDrive v0.2 slice adds authenticated file listing, folders, uploads, and downloads. The metadata schema now reserves durable records for versions, shares, resumable upload sessions, and an incremental change feed; service APIs for those records remain upcoming work. Broader monitoring and production hardening are still to come. The Docker image has not been run on the target Windows machine because WSL 2 is not installed.

Native Windows first-install setup is available at `scripts/setup-windows-postgres.ps1`. It invokes the PostgreSQL 17 installer when needed, provisions an isolated HomeOS database/role, builds the app, and registers HomeOS to start at Windows user sign-in. The installer and administrator-account creation require interactive password entry; see [local development](docs/development.md) for prerequisites and safeguards.

The first milestone (`v0.1`) will focus on HomeCore, authentication, the dashboard, and basic monitoring. See the [project concept and technical design report](HomeOS_Project_Report.md) for the full scope, architecture, roadmap, and risks.

The [v0.1 requirements and acceptance checklist](docs/v0.1-requirements.md) defines the first buildable milestone.

The [local development guide](docs/development.md) shows how to run the first working API and dashboard slice.

The [deployment and backup guide](docs/deployment.md) describes Docker packaging and SQLite backup/restore.

The [HomeDrive v0.2 guide](docs/v0.2-homedrive.md) documents its storage boundaries, API, large-file behavior, and separate file-backup requirement. The web app has a service sidebar; unfinished modules are explicitly marked as planned.

The [HomeDrive architecture addendum](HomeDrive_Architecture_Addendum.md) records the additive PostgreSQL, SQLite sync-client, Redis, and gRPC decisions. PostgreSQL support, the expanded metadata schema, an in-process metadata gRPC increment, opt-in Redis listing cache/login limits, and a [manual desktop sync prototype](docs/homedrive-sync-prototype.md) are implemented; full bidirectional sync, messaging, and observability remain staged work.

## Planned repository layout

```text
frontend/       React/TypeScript web application
backend/        Go application and database migrations
agents/         Restricted host integration agents
workers/        Optional background and AI workers
deploy/         Deployment configuration
docs/           Requirements and architecture documentation
tests/          Cross-component and acceptance tests
```

Directories will be added as their corresponding milestones begin; empty scaffolding is intentionally avoided.

## Contributing

The architecture and initial requirements are still being defined. Before implementing a large feature, open an issue so its scope, security implications, and milestone can be agreed upon. See [CONTRIBUTING.md](CONTRIBUTING.md) for the working conventions.

## Security

HomeOS will handle private data and privileged homelab operations. Do not report suspected vulnerabilities in a public issue. See [SECURITY.md](SECURITY.md).

## License

No open-source license has been selected yet. Until one is added, the repository contents remain under the copyright holder's default rights.
