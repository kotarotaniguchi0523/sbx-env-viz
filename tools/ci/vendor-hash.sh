#!/bin/bash
# vendor-hash.sh - verify or regenerate vendorHash in nix/package.nix after go.mod / go.sum change.
#
# Usage: tools/ci/vendor-hash.sh --check   exit 1 when vendorHash does not match the Go modules
#        tools/ci/vendor-hash.sh --write   write the hash Nix computes (used by relock.yml)
#
# Only a fixed-output hash mismatch counts as a stale vendorHash; any other build failure (network,
# a broken nixpkgs) is passed through as is. --write copies the "got:" value from Nix's mismatch
# report, so the new hash is the one Nix itself computed.
set -euo pipefail
mode="${1:-}"
case "$mode" in --check | --write) ;; *) echo "usage: vendor-hash.sh --check|--write" >&2; exit 2 ;; esac
cd "$(dirname "$0")/../.."
pkg=nix/package.nix
log="$(mktemp)"
trap 'rm -f "$log"' EXIT

if nix build .#sbx-env-viz.goModules --no-link -L >"$log" 2>&1; then
  echo "[ok] vendorHash matches go.mod / go.sum"
  exit 0
fi
got="$(sed -nE 's/^[[:space:]]*got:[[:space:]]*(sha256-[A-Za-z0-9+/=]+).*/\1/p' "$log" | head -1)"
if [[ -z "$got" ]]; then
  tail -40 "$log" >&2
  echo "nix build failed for a reason other than a vendorHash mismatch" >&2
  exit 1
fi
if [[ "$mode" == --check ]]; then
  echo "::error file=$pkg::vendorHash does not match go.mod / go.sum (Nix computed $got; run tools/ci/vendor-hash.sh --write)"
  exit 1
fi
# No confirming rebuild: ci.yml, dispatched on the branch after the push, builds it anyway.
sed -i.bak -E "s|vendorHash = \"[^\"]*\";|vendorHash = \"$got\";|" "$pkg" && rm -f "$pkg.bak"
echo "[ok] wrote vendorHash = $got"
