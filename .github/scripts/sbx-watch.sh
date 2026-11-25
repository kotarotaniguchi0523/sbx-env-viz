#!/usr/bin/env bash
# Detects a new stable docker/sbx-releases release that has been public for
# at least MIN_AGE_DAYS, collects what changed (CLI help, official docs,
# release notes), and opens one tracking issue for it.
#
# Inputs (environment):
#   GH_TOKEN       token for gh (issues: write)
#   GITHUB_REPOSITORY  owner/repo receiving the issue
#   MIN_AGE_DAYS   default 2
#   DRY_RUN        "1" prints the issue instead of creating it
#   FORCE          "1" ignores an existing issue
set -euo pipefail

MIN_AGE_DAYS="${MIN_AGE_DAYS:-2}"
SNAP_DIR=".github/sbx-snapshots"
TRACKED="$(tr -d '[:space:]' < .github/sbx-version)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cutoff=$(( $(date -u +%s) - MIN_AGE_DAYS * 86400 ))

gh api --paginate 'repos/docker/sbx-releases/releases?per_page=100' \
  --jq '.[] | select(.draft == false and .prerelease == false)
         | select(.tag_name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
         | [.tag_name, .published_at] | @tsv' > "$WORK/releases.tsv"

eligible="$WORK/eligible.txt"
: > "$eligible"
while IFS=$'\t' read -r tag published; do
  # Assigned on its own line so a date(1) failure stops the run under set -e
  # instead of making every release look ineligible.
  ts=$(date -u -d "$published" +%s)
  if [ "$ts" -le "$cutoff" ]; then
    echo "$tag" >> "$eligible"
  fi
done < "$WORK/releases.tsv"

latest="$(sort -V "$eligible" | tail -n 1)"
if [ -z "$latest" ]; then
  echo "no eligible stable release"; exit 0
fi
if [ "$(printf '%s\n%s\n' "$TRACKED" "$latest" | sort -V | tail -n 1)" = "$TRACKED" ]; then
  echo "tracked $TRACKED is current (latest eligible stable: $latest)"; exit 0
fi

title="sbx ${latest} に追従（現在の対象: ${TRACKED}）"
if [ "${FORCE:-0}" != "1" ]; then
  existing=$(gh issue list --repo "$GITHUB_REPOSITORY" --state all --label sbx-update --limit 100 \
    --json title --jq "[.[] | select(.title | startswith(\"sbx ${latest} \"))] | length")
  if [ "$existing" != "0" ]; then
    echo "issue for $latest already exists"; exit 0
  fi
fi
echo "new stable release: $latest (tracked $TRACKED)"

# --- CLI help diff -----------------------------------------------------------
help_section="$WORK/help.md"
{
  echo "## CLI ヘルプの差分（${TRACKED} のスナップショット → ${latest}）"
  echo
} > "$help_section"
case "$(uname -m)" in
  aarch64 | arm64) arch=arm64 ;;
  *) arch=amd64 ;;
esac
asset="DockerSandboxes-linux-${arch}.tar.gz"
if gh release download "$latest" -R docker/sbx-releases -p "$asset" -D "$WORK" 2>/dev/null; then
  mkdir -p "$WORK/sbx" "$WORK/new"
  tar -xzf "$WORK/$asset" -C "$WORK/sbx"
  bin="$(find "$WORK/sbx" -type f -name sbx | head -n 1)"
  changed=0
  for cmd in "env" "env create" "env plan" "env run" "env rm"; do
    f="$(echo "$cmd" | tr ' ' '-').txt"
    # shellcheck disable=SC2086
    env -u GH_TOKEN -u GITHUB_TOKEN HOME="$WORK/home" NO_COLOR=1 timeout 30 "$bin" $cmd --help > "$WORK/new/$f" 2>&1 || true
    if [ -f "$SNAP_DIR/$f" ]; then
      if ! diff -u --label "${TRACKED}/${f}" --label "${latest}/${f}" "$SNAP_DIR/$f" "$WORK/new/$f" > "$WORK/new/$f.diff"; then
        changed=1
        {
          echo "<details><summary><code>sbx ${cmd} --help</code> に差分あり</summary>"
          echo
          echo '```diff'
          head -c 12000 "$WORK/new/$f.diff"
          echo '```'
          echo "</details>"
          echo
        } >> "$help_section"
      fi
    else
      changed=1
      echo "- \`sbx ${cmd} --help\`: スナップショットがありません（新しい出力を \`${SNAP_DIR}/${f}\` に保存してください）" >> "$help_section"
    fi
  done
  [ "$changed" = 0 ] && echo "env 系コマンドの --help に差分はありません。" >> "$help_section"
else
  echo "Linux 版アーカイブを取得できなかったため、ヘルプ差分は未確認です。" >> "$help_section"
fi

# --- docs changes -------------------------------------------------------------
tracked_date="$(awk -F'\t' -v t="$TRACKED" '$1 == t { print $2 }' "$WORK/releases.tsv")"
docs_section="$WORK/docs.md"
{
  echo "## 公式 docs の変更（docker/docs: content/manuals/ai/sandboxes、${tracked_date:-不明} 以降）"
  echo
  if [ -n "$tracked_date" ]; then
    gh api "repos/docker/docs/commits?path=content/manuals/ai/sandboxes&since=${tracked_date}&per_page=50" \
      --jq '.[] | "- [`\(.sha[0:8])`](\(.html_url)) \(.commit.committer.date[0:10]) \(.commit.message | split("\n")[0])"' || echo "- 取得に失敗しました"
  else
    echo "- 追跡中の版の公開日が不明なため未取得"
  fi
  echo
} > "$docs_section"

# --- release notes --------------------------------------------------------------
notes_section="$WORK/notes.md"
echo "## リリースノート（${TRACKED} より新しい安定版、新しい順）" > "$notes_section"
# Newest first, so truncation drops the oldest notes, never the target's.
sort -rV "$eligible" | while read -r tag; do
  if [ "$(printf '%s\n%s\n' "$TRACKED" "$tag" | sort -V | tail -n 1)" = "$tag" ] && [ "$tag" != "$TRACKED" ]; then
    {
      echo
      echo "### ${tag}"
      gh api "repos/docker/sbx-releases/releases/tags/${tag}" --jq '.body' > "$WORK/body.txt"
      head -c 15000 "$WORK/body.txt"
      echo
    } >> "$notes_section"
  fi
done

body="$WORK/body.md"
{
  echo "docker/sbx-releases の新しい安定版 **${latest}**（公開から ${MIN_AGE_DAYS} 日以上経過）を検知しました。"
  echo "sbxray は現在 \`${TRACKED}\` の挙動を前提にしています（\`.github/sbx-version\` / \`pkg/sbxenv.TargetSbxVersion\`）。"
  echo
  echo "## 対応チェックリスト"
  echo "- [ ] 下の差分から、環境ファイル・kit 仕様・CLI フラグの変更点を洗い出す"
  echo "- [ ] \`pkg/sbxenv\`（スキーマ・マージ規則・診断）と \`docs_ja.go\` を更新する"
  echo "- [ ] テストを追加し \`go test -race ./...\` を通す"
  echo "- [ ] \`.github/sbx-snapshots/\` を新しい --help 出力で更新する"
  echo "- [ ] \`.github/sbx-version\` と \`TargetSbxVersion\` を \`${latest}\` に上げる"
  echo
  echo "> 自動実装に回す場合: この Issue にコメントでエージェント（例: \`@claude\`）を呼び出すか、T3 Code のスケジュールタスクの webhook から起動してください。"
  echo
  cat "$help_section" "$docs_section" "$notes_section"
} > "$body"
# Cut at 60000 bytes and drop a UTF-8 sequence the cut split.
head -c 60000 "$body" | iconv -f UTF-8 -t UTF-8 -c > "$body.trim"

if [ "${DRY_RUN:-0}" = "1" ]; then
  echo "---- $title"; cat "$body.trim"; exit 0
fi
gh label create sbx-update --repo "$GITHUB_REPOSITORY" --color 1d76db \
  --description "docker/sbx-releases の新しい安定版への追従" 2>/dev/null || true
gh issue create --repo "$GITHUB_REPOSITORY" --title "$title" --body-file "$body.trim" --label sbx-update
