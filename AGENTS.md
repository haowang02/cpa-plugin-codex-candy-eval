# Repository Instructions

## Scope and Layout

Inspect relevant source before changing integration contracts. Keep changes focused and preserve unrelated work.

- `../CLIProxyAPI/`: CPA host; `../Cli-Proxy-API-Management-Center/`: management console (CPAMC).
- `cmd/cpa-codex-candy-eval/`: shared-library entry point and C ABI bridge; `internal/plugin/`: handlers, runners, persistence, and tests.
- `internal/plugin/web/`: embedded UI and vendored renderers; `internal/fingerprint/`: fingerprint scoring and embedded reference data.
- `install.sh`, `install.ps1`: public installers; `.github/workflows/release.yml`: release checks, builds, and packaging.

## Build and Checks

For code changes, run `gofmt` on changed Go files, `go vet ./...`, and `go test -race ./...`. Update tests when behavior changes. For UI changes, also run `node scripts/test-ui.cjs`.

`scripts/deploy.sh` builds the Linux/amd64 CGO shared library with Zig (`brew install zig` on macOS), targeting glibc 2.17. Keep build artifacts in ignored `dist/` and cross-compilation variables scoped to each command. Verify the output is ELF x86-64 and loads in the CPA container.

## Private Configuration

Read deployment values from ignored `.env`; `.env.example` is a placeholder template. Keep `.env` at mode `600`, untracked, and out of uploads. Never expose credentials or private deployment values in logs, screenshots, commits, or reports.

`CPA_SSH_HOST` selects the server; `CPA_REMOTE_PLUGIN_DIR` and `CPA_REMOTE_COMPOSE_FILE` are relative to the remote user's home. `CPA_BASE_URL` and `CPA_MANAGEMENT_PASSWORD` provide the verification URL and login.

## Deployment and Verification

After plugin code, UI, build, or installer changes, complete the following without routine reconfirmation. Documentation and local setup changes are exempt. Preserve plugin state and history.

1. Before deploying a new `pluginVersion`, update CPA's `store.version` and `store.release-tag` pins; mismatched files are skipped by the loader.
2. Run `scripts/deploy.sh`. It builds, uploads with checksum verification, backs up existing versioned libraries to `plugin-backups/`, installs the versioned library, restarts only `cli-proxy-api`, and checks that the new file is registered and enabled. Inspect the server manually only if the script fails or the deployment layout changes.
3. Open `CPA_BASE_URL` in a browser, log in with the configured password, and verify the changed flow and authenticated API calls. Run one evaluation on one enabled account; confirm completion, results, and history after refresh. Minimize quota use; a wrong model answer is not a plugin failure.
4. If deployment breaks CPA or the plugin, restore the backup printed by the script under its original name, remove the new file, and restart through the same Compose file.

Report checks, artifact version/checksum, verification results, and any blockers or incomplete checks.

## Commits and Releases

Use an imperative commit subject; add a blank line and a body wrapped at 72 characters for nontrivial changes. Keep `pluginVersion` aligned with the `v<version>` release tag and follow `.github/workflows/release.yml`.
