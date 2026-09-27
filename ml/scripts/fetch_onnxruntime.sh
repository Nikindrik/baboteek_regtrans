#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="${ORT_VERSION:-1.30.0}"
LIBDIR="$ROOT/third_party/onnxruntime/lib"

ensure_links() {
  local real="$LIBDIR/libonnxruntime.so.${VER}"
  if [[ ! -f "$real" ]]; then
    return 1
  fi
  ln -sfn "libonnxruntime.so.${VER}" "$LIBDIR/libonnxruntime.so.1"
  ln -sfn "libonnxruntime.so.1" "$LIBDIR/libonnxruntime.so"
  return 0
}

# The final bundle already contains ORT. Avoid an unnecessary network download.
if ensure_links; then
  echo "ONNX Runtime ${VER} already present; linker aliases refreshed."
  exit 0
fi

ARCH="${ORT_ARCH:-$(uname -m)}"
case "$ARCH" in
  x86_64|amd64) PKG="onnxruntime-linux-x64-${VER}" ;;
  aarch64|arm64) PKG="onnxruntime-linux-aarch64-${VER}" ;;
  *) echo "Unsupported arch: $ARCH" >&2; exit 2 ;;
esac
URL="https://github.com/microsoft/onnxruntime/releases/download/v${VER}/${PKG}.tgz"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
echo "Downloading $URL"
curl -fL "$URL" -o "$TMP/ort.tgz"
tar -xzf "$TMP/ort.tgz" -C "$TMP"
rm -rf "$ROOT/third_party/onnxruntime/include" "$ROOT/third_party/onnxruntime/lib"
mkdir -p "$ROOT/third_party/onnxruntime"
cp -a "$TMP/$PKG/include" "$ROOT/third_party/onnxruntime/"
cp -a "$TMP/$PKG/lib" "$ROOT/third_party/onnxruntime/"
ensure_links
echo "ONNX Runtime ${VER} installed under third_party/onnxruntime"
