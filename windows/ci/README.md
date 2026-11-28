# CI for Windows

These two GitHub Actions workflows are stored here, inactive, because the account that pushed this branch could not create files under `.github/workflows` (GitHub requires the `workflow` scope). To turn them on, copy both files to `.github/workflows/` with an account that has that scope.

- `windows.yml` builds, tests and uploads the installer and zip on every change under `windows/`.
- `windows-release.yml` publishes a pre-release when you push a tag like `windows-v0.1.0`. Nothing is published until you do.
