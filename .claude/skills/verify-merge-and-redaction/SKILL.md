---
name: verify-merge-and-redaction
description: Check a change to sbxray's layer merging or strict redaction against the bounded exhaustive merge check and the redaction fuzzer. Use when editing pkg/sbxenv/merge.go, vnode.go, expand.go, redact.go, resolve.go (expansion), build.go or analyze.go (anything that formats values), or the redaction promise in README.md.
---

# Verify merge rules and strict redaction

Two promises users rely on are checked mechanically, not by example tests alone.

1. **Merge rules.** For every stack of up to 3 environment files with up to 2 `ports` entries each (5 entry shapes: sandbox port 1 or 2, protocol omitted, `TCP4`, `udp`, `tcp6`), the real `sbxenv.Resolve` produces exactly the list a reference model of the documented rule produces. The same holds for `cmd` over up to 4 files (omitted, `null`, `[]`, one or two items) and for `env` map merging over up to 4 files. Harm if broken: the tool shows a merged value sbx would not use.
2. **Strict redaction.** For any string planted in every value position, including through `${{ env.args }}` and kit arguments into structural fields, strict JSON never contains it. Harm if broken: `--json` output shared in CI or chat leaks a secret.

## Run

```sh
mise exec -- go test -run 'Exhaustively' -v ./pkg/sbxenv/          # about 1 s; logs how many stacks it checked
mise exec -- go test -run '^$' -fuzz FuzzStrictRedactionNeverEchoesValues -fuzztime 60s ./pkg/sbxenv/
```

`go test ./...` replays the fuzz corpus in `pkg/sbxenv/testdata/fuzz/` on every run. The weekly security-audit workflow fuzzes for 5 minutes.

## Reading a failure

- Exhaustive check: the message prints the smallest failing stack as layer lists, for example `stack [[] [{1 }] [{1 udp} {1 }]]`, which is three files whose ports are none; sandbox 1; sandbox 1/udp then sandbox 1. Rebuild that input by hand and run the CLI with `--text` to see the effective value. If the code is right and the reference model is wrong, fix the model in `pkg/sbxenv/merge_exhaustive_test.go` only after checking the rule in `.github/sbx-snapshots/env-create.txt` or the Docker Docs merge rules.
- Fuzzer: it prints the planted value and whether it leaked in the plan or an error. Run `go test -run=FuzzStrictRedactionNeverEchoesValues/<id> ./pkg/sbxenv/` and grep the JSON for the value to find the field. A value that is only a reference name like `${{ env.args.x }}` is structure and is excluded by the oracle.

Never weaken the reference model or the oracle to make a real defect pass. Report it with the counterexample.

## Proving the checks can fail

Both were shown failing on broken variants: list merge switched to append, a replaced entry moved to the end, protocol made case-sensitive, `cmd` appended, `null` overriding, maps replaced instead of merged (exhaustive check); expression bodies echoed in diagnostics and argument values echoed through `name`/`workspace`/kit domains (fuzzer). Re-run one such break after changing either harness.

## Coverage record

| Check | Covers |
|---|---|
| `TestPortsMergeMatchesTheReferenceModelExhaustively` | `merger.mergeInto`, `merger.mergeList`, `listKey`, `effectiveProtocol` in `pkg/sbxenv/merge.go`; port item formatting in `build.go` |
| `TestLaunchListsMatchTheReferenceModelExhaustively` | `listStrategy` replace path and null inheritance in `merge.go` |
| `TestScalarAndMapMergeMatchTheReferenceModelExhaustively` | map merge in `merge.go` |
| `FuzzStrictRedactionNeverEchoesValues` | `redactor` in `redact.go`, display forms (`vnode.disp`, read by `vnode.show` in `vnode.go`) set in `resolve.go` and `kits.go`, every formatter in `build.go`, messages in `analyze.go` and `kits.go` |

Not covered: `mcp.servers` and `additionalWorkspaces` keyed merges share `mergeList` but only ports are enumerated; kit `args` merging; the redaction of literal structure (paths, ports, domains written directly in a file), which is shown by design.

Last confirmed against the code at commit 994b227. Move this forward only after re-reading the covered functions and re-running both checks at the new commit.
