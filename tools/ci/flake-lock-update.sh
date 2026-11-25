#!/bin/bash
# flake-lock-update.sh - move flake.lock to the newest commit of each input that is at least
# MIN_AGE_DAYS old (adopt releases only after 2 days, like mise and Renovate). Never runs
# `nix flake update`, which would take the newest commit regardless of age.
#
# Usage: tools/ci/flake-lock-update.sh        (needs nix, jq, gh with a token)
# Prints "changed=true|false" (also to $GITHUB_OUTPUT when set).
# Limit: the age is the commit date on the branch, not the time the channel branch advanced.
set -euo pipefail
cd "$(dirname "$0")/../.."
MIN_AGE_DAYS="${MIN_AGE_DAYS:-2}"
until="$(date -u -d "-${MIN_AGE_DAYS} days" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
  date -u -v-"${MIN_AGE_DAYS}"d +%Y-%m-%dT%H:%M:%SZ)"

# Every direct input of the flake, with the branch it follows (flake.lock "original").
args=()
while read -r input owner name ref; do
  sha="$(gh api -X GET "repos/$owner/$name/commits" -f "sha=refs/heads/$ref" -f "until=$until" -f per_page=1 --jq '.[0].sha')"
  if [[ -z "$sha" ]]; then
    echo "no commit on $owner/$name@$ref before $until" >&2
    exit 1
  fi
  echo "[i] $input: $owner/$name@$ref -> ${sha:0:12} (<= $until)"
  args+=(--override-input "$input" "github:$owner/$name/$sha")
done < <(jq -r '.nodes as $n | $n.root.inputs | to_entries[]
  | "\(.key) \($n[.value].original | "\(.owner) \(.repo) \(.ref)")"' flake.lock)

nix flake lock "${args[@]}"

changed=false
git diff --quiet -- flake.lock || changed=true
echo "changed=$changed"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then echo "changed=$changed" >> "$GITHUB_OUTPUT"; fi
exit 0
