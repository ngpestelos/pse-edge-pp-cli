---
name: pp-pse-edge
description: "Agent-native Philippine Stock Exchange CLI — quotes, filings, and a local price history no free API serves. Trigger phrases: `quote AT`, `how is the PSEi doing`, `PSE market breadth`, `check PSE disclosures for GTCAP`, `has the 17-Q been filed`, `PSE movers this week`, `use pse-edge`, `run pse-edge`."
version: "1.2.0"
author: "Nestor G Pestelos Jr"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  hermes:
    tags: [PSE, Philippine Stock Exchange, EDGE, CLI, finance]
    related_skills: [pse-edge, pse-edge-chrome, pse-data-sourcing, investor-vault-operations, ship-go-cli-fleet]
  openclaw:
    requires:
      bins:
        - pse-edge-pp-cli
    install:
      - kind: go
        bins: [pse-edge-pp-cli]
        module: github.com/ph-commons/pse-edge-pp-cli/cmd/pse-edge-pp-cli
---

# PSE Edge — Printing Press CLI

Structured JSON over PSE EDGE + compositeSector + Phisix convenience + local SQLite history. **No auth.** Read-only markets.

Public repo: https://github.com/ph-commons/pse-edge-pp-cli

## Install / verify

```bash
which pse-edge-pp-cli || true
pse-edge-pp-cli --version
```

Missing or **stale** (release **v0.1.0**+):

1. Prebuilt: `curl -fsSL https://raw.githubusercontent.com/ngpestelos/pse-edge-pp-cli/main/scripts/install.sh | bash`
2. First-time fleet: `rebuild` (dotfiles `installPrebuiltCli` skips if binary already present)
3. Else Go ≥1.26.6: `go install github.com/ph-commons/pse-edge-pp-cli/cmd/pse-edge-pp-cli@latest`

**Do not run skill commands until `--version` works.** Prefer release tarball (`0.1.0+`); bare `go install` may show module/VCS pseudo-version, not forever-`0.0.0-dev` after resolveVersion.

MCP: `go install …/pse-edge-pp-mcp@latest` → `claude mcp add pse-edge-pp-mcp -- pse-edge-pp-mcp`. Relocate via host env `PSE_EDGE_HOME`.

## Anti-triggers

| Need | Use instead |
|------|-------------|
| Intraday / tick / VWAP | none free — EOD only |
| Orders / broker | not this CLI |
| Non-PH exchanges | other tools |
| Browser-only EDGE tabs | `pse-edge-chrome` |
| Curl endpoint forensics | `pse-edge` |
| Authoritative personal tape | vault journal (`investor-vault-operations`) |

## Core commands

| Job | Command |
|-----|---------|
| Calendar / as-of gate | `pse-edge-pp-cli session --json` |
| PSEi + breadth + totals | `pse-edge-pp-cli market --json` |
| Live quote (dual source) | `pse-edge-pp-cli quote AT GTCAP --json` |
| Resolve symbol → ids | `pse-edge-pp-cli resolve GTCAP --json` |
| Company profile | `pse-edge-pp-cli company AT --json` |
| Sync registry + EOD | `pse-edge-pp-cli sync market --symbols AT,GTCAP` |
| Local OHLC | `pse-edge-pp-cli history AT --since 30d --json` |
| vs PSEi + 52w band | `pse-edge-pp-cli drift AT --since 90d --json` |
| Gainers/losers | `pse-edge-pp-cli movers --since 7d --json` |
| Breadth series | `pse-edge-pp-cli breadth --since 30d --json` |
| Filings (search index) | `pse-edge-pp-cli filings AT --json` |
| Filing by edge_no (viewer) | `pse-edge-pp-cli filings get --edge-no <hash> --json` |
| Latest filing file_id + body | `pse-edge-pp-cli filings latest-body AT --json` |
| Export local EOD/index | `pse-edge-pp-cli export eod\|index --from YYYY-MM-DD --format jsonl` |
| 17-Q/17-A deadlines | `pse-edge-pp-cli deadlines AT --json` |
| Typed financials | `pse-edge-pp-cli financials AT --json` |
| Stale local data | `pse-edge-pp-cli stale --json` |
| Capability routing | `pse-edge-pp-cli which "history"` |
| Health | `pse-edge-pp-cli doctor` · `doctor --fail-on warn` |

`--agent` ≡ machine defaults. Exit 2 = usage (incl. bad symbol shape); 3 = not found / empty local series when treated as error.

## Hard domain rules

- **GTCAP common** = `cmpy_id=633`, `security_id=572`. **628 = GTPPB preferred** (~2× price). Never hardcode 628 for GTCAP common.
- Gate EOD on `session` — blank change fields on non-trading days are **states**, not zeros.
- `history` / `drift` / `movers` / `breadth` / `deadlines` need `sync market` when the local store is empty.
- Announcements search: server ignores free-text `keyword` — CLI filters titles client-side.
- Filings search is **not** an authoritative complete corpus (`complete` is relative to `announcements/search.ax` only). Prefer `filings get --edge-no` when a viewer URL is known. Search rows have `edge_no`, not `file_id`; use `filings latest-body SYMBOL` for newest `file_id` + body.
- Phisix official API gone 2023-12-04; api3 is convenience overlay, not first-party.

## Recipes

```bash
# Morning tape
pse-edge-pp-cli market --json --select psei,breadth.advances,breadth.declines,totals.value

# Position review with as-of
pse-edge-pp-cli quote AT GTCAP HTI --agent --select symbol,close,change_pct,as_of,stale,source

# Filing status
pse-edge-pp-cli deadlines AT --json
pse-edge-pp-cli filings GTCAP --from-date 01-01-2026 --json
# Always read warnings/complete/freshness_gap_days — search is not a complete corpus.
# Known edge_no missing from search:
pse-edge-pp-cli filings get --edge-no 2bc053ab3b1339fb64d70b69f0a3140b --json
pse-edge-pp-cli filings latest-body GTCAP --json

# Relative strength
pse-edge-pp-cli drift AT --since 90d --agent
```

## Agent mode

`--agent` → JSON + compact + no prompts. Prefer `--select` for small payloads.

Generated resource commands wrap `{meta, results}`; novel local-store commands (`history`, `breadth`, `movers`, `stale`, `quote`, …) emit bare JSON with inline source/as_of/stale.

## Paths

- `--home` / `PSE_EDGE_HOME` relocates config/data/state/cache.
- Per-kind: `PSE_EDGE_CONFIG_DIR`, `PSE_EDGE_DATA_DIR`, `PSE_EDGE_STATE_DIR`, `PSE_EDGE_CACHE_DIR` (override `--home` for that kind).
- MCP: set `PSE_EDGE_HOME` in host env (MCP does not inherit CLI flags).
- Relocation is not reversible by unsetting env alone — move files first.

## Learning loop (judgment only)

CLI journals invocations and derives candidates; agent does **not** hand-record failures.

1. **`recall` first** on a new question (skip for rest of session if store is fully cold).
2. Decision order: candidates → playbook/notes → exact resource hit → cold start.
3. Always read `warnings`.
4. After answering: `teach &` (background) with structural query, no PII.
5. Optional: playbook flags on `teach`, or `playbook amend &` for observed gotchas only.

```
if Candidates present (warnings include "candidates_present"):
    -> candidates are try-then-confirm, never facts. Follow each candidate's
       two-step next_action verbatim: run the trial command first, then run
       `learnings confirm <id>` only after the trial verified the behavior.
       Reject a wrong candidate with `learnings reject <id>`.
    -> NEVER re-teach something recall surfaced as a candidate; confirm or
       reject that candidate instead of teaching a duplicate.
```

Disable: `--no-learn` or `PSE_EDGE_NO_LEARN=true`.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage |
| 3 | Not found |
| 5 | API error |
| 7 | Rate limited |
| 10 | Config error |

## Direct use

1. `which pse-edge-pp-cli` — install if missing.
2. Match Unique/Core table or `which "<capability>"`.
3. Run with `--agent`.
4. Ambiguous → `<cmd> --help`.
