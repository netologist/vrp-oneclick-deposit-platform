#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if compgen -G "$ROOT/logs/*.pid" > /dev/null; then
  for p in "$ROOT"/logs/*.pid; do
    pid=$(cat "$p" 2>/dev/null || true)
    if [[ -n "${pid:-}" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      echo "stopped $pid ($(basename "$p" .pid))"
    fi
    rm -f "$p"
  done
fi
pkill -f "$ROOT/bin/" 2>/dev/null || true
echo "done"
