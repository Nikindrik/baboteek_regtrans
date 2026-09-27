#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! command -v sphinx-build >/dev/null 2>&1; then
  echo "sphinx-build not found. Install: pip install -r docs/requirements.txt" >&2
  exit 1
fi

sphinx-build -b html docs docs/_build/html
echo "Sphinx HTML: $ROOT/docs/_build/html/index.html"
