#!/bin/bash
# mise-lock.sh - verify or regenerate mise.lock for the pinned tools in mise.toml.
#
# Usage: tools/ci/mise-lock.sh --check   exit 1 when mise.lock is missing entries or out of date
#        tools/ci/mise-lock.sh --write   update mise.lock in place (used by relock.yml)
#
# The lock is regenerated on a copy in a temp dir, so --check never touches the working tree.
set -euo pipefail
mode="${1:-}"
case "$mode" in --check | --write) ;; *) echo "usage: mise-lock.sh --check|--write" >&2; exit 2 ;; esac
repo="$(cd "$(dirname "$0")/../.." && pwd)"
# pwd -P: on macOS mktemp returns /var/..., which mise resolves to /private/var/... when it
# checks trust. The global config is left out so tools installed on the host cannot leak in.
work="$(cd "$(mktemp -d)" && pwd -P)"
trap 'rm -rf "$work"' EXIT
mkdir "$work/global"
export MISE_TRUSTED_CONFIG_PATHS="$work" MISE_CONFIG_DIR="$work/global" MISE_GLOBAL_CONFIG_FILE="$work/global/config.toml"
cp "$repo/mise.toml" "$repo/mise.lock" "$work/"
(cd "$work" && mise lock >/dev/null)
if cmp -s "$work/mise.lock" "$repo/mise.lock"; then
  echo "[ok] mise.lock is up to date"
  exit 0
fi
if [[ "$mode" == --write ]]; then
  cp "$work/mise.lock" "$repo/mise.lock"
  echo "[ok] wrote mise.lock"
  exit 0
fi
echo "::error file=mise.lock::mise.lock is out of date (run tools/ci/mise-lock.sh --write)"
diff -u "$repo/mise.lock" "$work/mise.lock" || true
exit 1
