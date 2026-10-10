# Contributing to HomeOS

HomeOS is in its planning stage. Contributions should keep the project incremental, secure, and portable.

## Before making a change

1. Check the project report and existing issues for relevant scope.
2. Open an issue before substantial features or architectural changes.
3. Keep pull requests focused on one concern.
4. Never commit credentials, private keys, personal data, runtime databases, or backups.

## Development expectations

- Preserve clear boundaries between core services and optional modules.
- Add tests for new behavior and bug fixes.
- Document configuration and security-sensitive behavior.
- Use conventional commit-style subjects where practical, such as `feat:`, `fix:`, `docs:`, `test:`, and `chore:`.
- Treat destructive operations, filesystem access, and privileged host actions as security-sensitive.

More specific setup, test, and lint commands will be documented once the initial application skeleton is created.
