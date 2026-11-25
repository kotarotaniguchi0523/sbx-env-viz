#!/bin/bash
# vendor-hash.sh - verify or regenerate vendorHash in nix/package.nix after go.mod / go.sum change.
#
# Usage: tools/ci/vendor-hash.sh --check   exit 1 when vendorHash does not match the Go modules
#        tools/ci/vendor-hash.sh --write   write the hash Nix computes (used by go-relock.yml)
#
# --write builds the module derivation with a placeholder hash and copies the "got:" value
# from Nix's mismatch report, so the new hash is what Nix itself computed.
set -euo pipefail
mode="${1:-}"
case "$mode" in --check | --write) ;; *) echo "usage: vendor-hash.sh --check|--write" >&2; exit 2 ;; esac
cd "$(dirname "$0")/../.."
pkg=nix/package.nix
attr=".#sbx-env-viz.goModules"

if nix build "$attr" --no-link 2>/dev/null; then
  echo "[ok] vendorHash matches go.mod / go.sum"
  exit 0
fi
if [[ "$mode" == --check ]]; then
  echo "::error file=$pkg::vendorHash does not match go.mod / go.sum (run tools/ci/vendor-hash.sh --write)"
  nix build "$attr" --no-link -L 2>&1 | tail -20 || true
  exit 1
fi

placeholder="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
sed -i.bak -E "s|vendorHash = \"[^\"]*\";|vendorHash = \"$placeholder\";|" "$pkg" && rm -f "$pkg.bak"
got="$(nix build "$attr" --no-link 2>&1 | sed -nE 's/^[[:space:]]*got:[[:space:]]*(sha256-[A-Za-z0-9+/=]+).*/\1/p' | head -1)"
if [[ -z "$got" ]]; then
  echo "could not read the vendorHash Nix computed" >&2
  git checkout -- "$pkg"
  exit 1
fi
sed -i.bak -E "s|vendorHash = \"$placeholder\";|vendorHash = \"$got\";|" "$pkg" && rm -f "$pkg.bak"
nix build "$attr" --no-link
echo "[ok] wrote vendorHash = $got"
