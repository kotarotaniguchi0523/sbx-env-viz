"""Select CI checks from the complete PR diff. Unknown inputs run every check."""

import os
import subprocess

ALL = set("test mutation e2e build snapshot nix lock workflows vuln".split())


def checks(path):
    if path in {"mise.toml", "mise.lock", ".github/workflows/ci.yml"} or path.startswith("tools/ci/select-checks"):
        return ALL
    if path in {"go.mod", "go.sum"} or path.startswith(("cmd/", "internal/", "pkg/")):
        selected = {"test", "e2e", "build", "snapshot", "nix", "vuln"}
        if path in {"go.mod", "go.sum"} or path.startswith("internal/parser/"):
            selected.add("mutation")
        return selected
    if path.startswith("e2e/tui/"):
        return {"test", "e2e", "vuln"}
    if path.startswith("testdata/"):
        return {"test", "e2e"}
    if path.startswith((".github/sbx-version", ".github/sbx-snapshots/")):
        return {"test"}
    if path in {"flake.nix", "flake.lock", "tools/ci/vendor-hash.sh"} or path.startswith("nix/"):
        return {"nix"}
    if path == "tools/ci/mise-lock.sh":
        return {"lock"}
    if path in {"README.md", "LICENSE", ".goreleaser.yaml"}:
        return {"snapshot"}
    if path.startswith(".github/actions/"):
        return ALL
    if path.startswith(".github/workflows/"):
        return {"workflows"}
    if path in {"DESIGN.md", "AGENTS.md", "CLAUDE.md", "README.ja.md"} or path.startswith(("skills/", "docs/")):
        return set()
    return ALL


def main():
    selected = ALL
    if os.environ["GITHUB_EVENT_NAME"] == "pull_request":
        diff = subprocess.run(
            ["git", "diff", "--no-renames", "--name-only", "-z",
             os.environ["BASE_SHA"] + "..." + os.environ["HEAD_SHA"], "--"],
            stdout=subprocess.PIPE,
        )
        if diff.returncode == 0:
            selected = set()
            for path in diff.stdout.split(b"\0"):
                if path:
                    selected.update(checks(os.fsdecode(path)))
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        for job in sorted(ALL):
            print(f"{job}={str(job in selected).lower()}", file=output)


if __name__ == "__main__":
    main()
