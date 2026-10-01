#!/usr/bin/env bash
# Build the Linux/amd64 plugin, install it on the CPA server from .env,
# restart CPA and check that the new library loaded.
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; . ./.env; set +a

id=cpa-codex-candy-eval
service=cli-proxy-api
version=$(sed -n 's/^[[:space:]]*pluginVersion[[:space:]]*= "\(.*\)"/\1/p' internal/plugin/plugin.go)
target=$id-v$version.so
backups=${CPA_REMOTE_PLUGIN_DIR%/plugins/*}/plugin-backups
stamp=$(date -u +%Y%m%d-%H%M%S)

echo "==> Building $target"
mkdir -p dist
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CC='zig cc -target x86_64-linux-gnu.2.17' \
  go build -trimpath -ldflags='-s -w' -tags cshared -buildmode=c-shared -o "dist/$id.so" "./cmd/$id"
sum=$(shasum -a 256 "dist/$id.so" | cut -d' ' -f1)
echo "sha256 $sum"

echo "==> Installing on the CPA server"
scp -q "dist/$id.so" "$CPA_SSH_HOST:$CPA_REMOTE_PLUGIN_DIR/.$id.upload"
ssh "$CPA_SSH_HOST" bash -s -- "$CPA_REMOTE_PLUGIN_DIR" "$backups" "$id" "$target" "$sum" "$stamp" "$CPA_REMOTE_COMPOSE_FILE" "$service" <<'EOF'
set -euo pipefail
dir=$1 backups=$2 id=$3 target=$4 sum=$5 stamp=$6 compose=$7 service=$8
echo "$sum  $dir/.$id.upload" | sha256sum -c --quiet -
mkdir -p "$backups"
for file in "$dir/$id"-v*.so; do
  [ -e "$file" ] || continue
  mv "$file" "$backups/${file##*/}.$stamp"
  echo "backup plugin-backups/${file##*/}.$stamp"
done
mv "$dir/.$id.upload" "$dir/$target"
docker compose -f "$compose" restart "$service"
docker compose -f "$compose" ps "$service"
EOF

echo "==> Checking plugin status"
curl -fsS --retry 20 --retry-delay 2 --retry-all-errors -H "Authorization: Bearer $CPA_MANAGEMENT_PASSWORD" "$CPA_BASE_URL/v0/management/plugins" |
  jq -e --arg id "$id" --arg target "$target" \
    '.plugins[] | select(.id == $id and .registered and .effective_enabled and (.path | endswith("/" + $target))) | {path, registered, effective_enabled}' ||
  { echo "$target did not load; check the store.version pin or restore the backup above." >&2; exit 1; }
