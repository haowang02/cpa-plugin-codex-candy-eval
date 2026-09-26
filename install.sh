#!/bin/sh
# Install the latest release into ./plugins/<os>/<arch>. Run it in the CLIProxyAPI working directory.
set -eu

repo="haowang02/cpa-plugin-codex-candy-eval"
name="cpa-codex-candy-eval"

fail() {
  echo "$name: $*" >&2
  exit 1
}

case "$(uname -s)" in
  Linux) os=linux ext=so ;;
  Darwin) os=darwin ext=dylib ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

tag="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")" || fail "failed to resolve the latest release"
tag="${tag##*/}"
version="${tag#v}"
asset="${name}_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"
dir="plugins/$os/$arch"
file="$name-v$version.$ext"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset..."
curl -fsSL -o "$tmp/$asset" "$base/$asset" || fail "failed to download $base/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "failed to download checksums.txt"
expected="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")"
actual="$({ sha256sum "$tmp/$asset" 2>/dev/null || shasum -a 256 "$tmp/$asset"; } | awk '{ print $1 }')"
[ -n "$expected" ] && [ "$expected" = "$actual" ] || fail "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$dir"
# Stage next to the target and rename, so a running CLIProxyAPI never sees a partial file.
cp "$tmp/$name.$ext" "$dir/.$file.tmp"
mv -f "$dir/.$file.tmp" "$dir/$file"
echo "Installed: $(pwd)/$dir/$file"
