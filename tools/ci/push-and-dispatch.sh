#!/bin/bash
# push-and-dispatch.sh - commit <files> as github-actions[bot], push them to <branch>, then start
# ci.yml there (pushes made with GITHUB_TOKEN do not trigger workflows by themselves).
#
# Usage: tools/ci/push-and-dispatch.sh <branch> <expected-sha|-> <message> <file>...  (needs GH_TOKEN)
#
# Nothing happens when <file>... has no changes. The push replaces the branch only while it still
# points at <expected-sha> (--force-with-lease): pass the PR head the job checked out, so a commit
# Renovate pushed meanwhile is never overwritten (the next run relocks it), or "-" for a branch
# only this workflow writes, which takes whatever the branch holds now (or creates it). Renovate
# keeps rebasing branches with these commits: their author is in renovate.json gitIgnoredAuthors.
set -euo pipefail
branch="${1:?branch}" expected="${2:?expected sha or -}" message="${3:?message}"
shift 3
if [[ -z "$(git status --porcelain -- "$@")" ]]; then
  echo "[i] $* already up to date"
  exit 0
fi
if [[ "$expected" == - ]]; then
  expected="$(git ls-remote origin "refs/heads/$branch" | cut -f1)"
fi
git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git switch -C "$branch"
git add -A -- "$@"
git commit -m "$message" -- "$@"
git push --force-with-lease="refs/heads/$branch:$expected" origin "HEAD:refs/heads/$branch"
gh workflow run ci.yml --ref "$branch"
