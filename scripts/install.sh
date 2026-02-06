#!/usr/bin/env sh
set -eu

usage() {
  cat <<EOF
Usage: install.sh [--version <latest|vX.Y.Z|X.Y.Z>] [--repo <owner/repo>] [--install-dir <path>]

Examples:
  install.sh
  install.sh --version latest
  install.sh --version v0.2.0
EOF
}

REPO="${REPO:-CharlieAIO/sol-cloud}"
VERSION_INPUT="${SOL_CLOUD_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version|-v)
      [ "$#" -ge 2 ] || { echo "missing value for $1" >&2; exit 1; }
      VERSION_INPUT="$2"
      shift 2
      ;;
    --repo|-r)
      [ "$#" -ge 2 ] || { echo "missing value for $1" >&2; exit 1; }
      REPO="$2"
      shift 2
      ;;
    --install-dir|-d)
      [ "$#" -ge 2 ] || { echo "missing value for $1" >&2; exit 1; }
      INSTALL_DIR="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

uname_s="$(uname -s)"
uname_m="$(uname -m)"

case "$uname_s" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *)
    echo "unsupported OS: $uname_s (expected Linux or Darwin)" >&2
    exit 1
    ;;
esac

case "$uname_m" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "unsupported arch: $uname_m (expected x86_64/amd64 or arm64/aarch64)" >&2
    exit 1
    ;;
esac

resolve_latest_tag() {
  api="https://api.github.com/repos/${REPO}/releases/latest"
  tag="$(curl -fsSL "$api" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  if [ -n "$tag" ]; then
    printf '%s\n' "$tag"
    return 0
  fi

  curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" | sed 's#.*/tag/##'
}

case "$(printf '%s' "$VERSION_INPUT" | tr '[:upper:]' '[:lower:]')" in
  latest)
    tag="$(resolve_latest_tag)"
    ;;
  *)
    case "$VERSION_INPUT" in
      v*) tag="$VERSION_INPUT" ;;
      *) tag="v$VERSION_INPUT" ;;
    esac
    ;;
esac

[ -n "$tag" ] || { echo "failed to resolve release tag" >&2; exit 1; }

asset="sol-cloud_${tag}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

echo "downloading ${url}"
curl -fL "$url" -o "$tmpdir/$asset"
tar -xzf "$tmpdir/$asset" -C "$tmpdir"

target_bin="$INSTALL_DIR/sol-cloud"
if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR"
  target_bin="$INSTALL_DIR/sol-cloud"
  echo "install dir not writable, using ${INSTALL_DIR}" >&2
fi

mv "$tmpdir/sol-cloud" "$target_bin"
chmod +x "$target_bin"

echo "installed: $target_bin"
"$target_bin" --version || true

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo "note: add ${INSTALL_DIR} to your PATH" >&2
    ;;
esac
