# HomeOS

HomeOS is a planned self-hosted personal cloud and homelab management platform. It aims to provide one modular web interface for files, media, monitoring, containers, backups, automation, and related services.

The proposed foundation is:

- Go modular-monolith backend
- React and TypeScript frontend
- SQLite metadata storage
- Shared filesystem storage
- Docker Compose deployment

## Project status

HomeOS is in early v0.1 development. It currently has a loopback-only Go API, SQLite-backed administrator authentication, a React dashboard with native Windows host metrics, and Docker Compose packaging. Broader monitoring and production hardening are still to come.

The first milestone (`v0.1`) will focus on HomeCore, authentication, the dashboard, and basic monitoring. See the [project concept and technical design report](HomeOS_Project_Report.md) for the full scope, architecture, roadmap, and risks.

The [v0.1 requirements and acceptance checklist](docs/v0.1-requirements.md) defines the first buildable milestone.

The [local development guide](docs/development.md) shows how to run the first working API and dashboard slice.

The [deployment and backup guide](docs/deployment.md) describes Docker packaging and SQLite backup/restore.

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
