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
- `internal/plugin/web/vendor/`: vendored Markdown (markdown-it with
  markdown-it-cjk-friendly) and math (markdown-it-texmath, Temml) renderers
  for candy answers.
- `internal/plugin/data/`: embedded fingerprint probes and baselines.
- `docs/images/`: README screenshots.
- `docs/inspection.md`, `examples/inspection.py`: scheduled inspection guide
  and its standard-library Python script.
- Root `install.sh` and `install.ps1`: public installation entry points.
- `scripts/`: one-command deployment (`deploy.sh`) and UI checks (`test-ui.cjs`).
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
`scripts/deploy.sh` runs this same build before deploying.

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

1. Run `scripts/deploy.sh`. It builds the Linux/amd64 library, uploads it to
   `CPA_REMOTE_PLUGIN_DIR` with a checksum check, moves existing
   `cpa-codex-candy-eval-v*.so` files to `plugin-backups/` beside the remote
   `plugins/` directory, installs `cpa-codex-candy-eval-v<pluginVersion>.so`,
   restarts only the `cli-proxy-api` Compose service, and fails unless the
   management API reports the new file registered and enabled. Plugin state and
   history are untouched. Inspect the server by hand only when the script fails
   or the deployment layout changes. Before deploying a new `pluginVersion`,
   update CPA's `store.version` and `store.release-tag` pins; the loader skips
   files that do not match them.
2. Open `CPA_BASE_URL` in a browser and authenticate using the configured
   password. Verify the changed flow and authenticated API calls. Run one
   evaluation on one enabled account; confirm completion, results, and history
   after refresh. Keep quota use minimal; a wrong model answer is not a plugin
   failure.
3. If deployment breaks CPA or the plugin, move the backup printed by the script
   back under its original name, remove the new file, and restart through the
   same Compose file. Report checks, artifact version/checksum, and
   verification results; explicitly identify blockers and incomplete checks.

## Commits and Releases

Use an imperative subject; for nontrivial changes, add a blank line and a body
wrapped at 72 characters. Small changes need only a subject. For releases, keep
`pluginVersion` aligned with the `v<version>` tag and follow the release workflow.
