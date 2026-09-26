# Repository Instructions

## References and Scope

- CPA host: `../CLIProxyAPI/`.
- Management console (CPAMC): `../Cli-Proxy-API-Management-Center/`.
- Build and release conventions: `.github/workflows/release.yml`.
- Inspect relevant source before changing integration contracts. Keep changes
  focused and preserve unrelated work.

## Project Layout

- `cmd/cpa-codex-candy-eval/`: shared-library entry point and C ABI bridge.
- `internal/plugin/`: plugin handlers, test runners, persistence, and Go tests.
- `internal/plugin/web/`: embedded management page.
- `internal/plugin/data/`: embedded fingerprint probes and baselines.
- `docs/images/`: README screenshots.
- Root `install.sh` and `install.ps1`: public installation entry points.
- `.github/workflows/`: release checks, platform builds, and packaging.
- `dist/`: ignored build artifacts.

## Build and Checks

Run `gofmt` on changed Go files; run `go vet ./...` and `go test -race ./...`
for code changes. Update tests when behavior changes.

On macOS, install Zig with `brew install zig`. `zig cc` is Zig's C compiler
subcommand; use it with Go to cross-compile this CGO plugin for Linux/amd64:

```sh
mkdir -p dist
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
  CC='zig cc -target x86_64-linux-gnu.2.17' \
  go build -trimpath -ldflags='-s -w' -tags cshared -buildmode=c-shared \
  -o dist/cpa-codex-candy-eval.so ./cmd/cpa-codex-candy-eval
file dist/cpa-codex-candy-eval.so
```

Expect an ELF x86-64 shared library. The glibc 2.17 target matches the release
baseline; verify compatibility with the CPA container. Keep artifacts in
ignored `dist/`. Set cross-compilation variables per command, not globally.

## Private Configuration

Read deployment values from ignored `.env`; use `.env.example` only as a
placeholder template. Keep `.env` at mode `600`, untracked, and out of uploads.
Never expose credentials or private deployment values in logs, screenshots,
commits, or reports.

- `CPA_SSH_HOST`: deployment SSH host.
- `CPA_REMOTE_PLUGIN_DIR`, `CPA_REMOTE_COMPOSE_FILE`: paths relative to the
  remote SSH user's home, resolved on the server.
- `CPA_BASE_URL`, `CPA_MANAGEMENT_PASSWORD`: public verification origin and login.

## Deployment and End-to-End Verification

After plugin code, UI, build, or installation changes, complete these steps
without routine reconfirmation. Documentation and local setup changes are exempt.

1. Build the updated Linux/amd64 library. Inspect the remote Compose file,
   service name, plugin mount, and active version before deploying.
2. Back up the current library outside the loader's search paths. Upload to
   `CPA_REMOTE_PLUGIN_DIR`, verify the checksum, then atomically rename to
   `cpa-codex-candy-eval-v<pluginVersion>.so`. Preserve state and history; ensure
   plugin enablement and any `store.version`/`store.release-tag` pins select it.
3. Restart only CPA via `docker compose -f <resolved CPA_REMOTE_COMPOSE_FILE>`
   on `CPA_SSH_HOST`. Check service status and plugin-loading logs.
4. Open `CPA_BASE_URL` in a browser and authenticate using the configured
   password. Verify the changed flow and authenticated API calls. Run one
   evaluation on one enabled account; confirm completion, results, and history
   after refresh. Keep quota use minimal; a wrong model answer is not a plugin
   failure.
5. Restore the backup and restart through the same Compose file if deployment
   breaks CPA or the plugin. Report checks, artifact version/checksum, and
   verification results; explicitly identify blockers and incomplete checks.

## Commits and Releases

Use an imperative subject; for nontrivial changes, add a blank line and a body
wrapped at 72 characters. Small changes need only a subject. For releases, keep
`pluginVersion` aligned with the `v<version>` tag and follow the release workflow.
