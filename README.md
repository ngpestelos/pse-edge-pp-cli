# PSE Edge CLI

**Agent-native Philippine Stock Exchange CLI — quotes, filings, and a local price history no free API serves.**

Every figure carries its source and as-of trading date, so agents can quote the tape without narrating stale prices. Sync builds a local SQLite registry and EOD history (backfillable to 2021 for the PSEi), unlocking history, drift, breadth, and movers commands that no PSE endpoint can answer. Dual-sourced quotes survive either upstream dying.

Created by [Nestor G Pestelos Jr](https://npestelos.com).

> **Unofficial.** Independent community tool — **not affiliated with, endorsed by, or supported by the Philippine Stock Exchange**. It reads publicly available market data and disclosure HTML; upstream pages and endpoints can change without notice. For official market data and filings, use [edge.pse.com.ph](https://edge.pse.com.ph) and PSE-authorized feeds. This tool is for research automation, not life-safety or regulatory-submission workflows.

## Install

Requires [Go 1.26.6 or newer](https://go.dev/dl/):

```bash
go install github.com/ph-commons/pse-edge-pp-cli/cmd/pse-edge-pp-cli@latest
```

The binary installs to `$(go env GOPATH)/bin` (usually `~/go/bin`); make sure that's on your `PATH`.

Or use the one-shot installer (idempotent; prefers the prebuilt GitHub release so machines never compile `modernc.org/sqlite`; falls back to `go install`; installs to `~/.local/bin`):

```bash
curl -fsSL https://raw.githubusercontent.com/ph-commons/pse-edge-pp-cli/main/scripts/install.sh | bash
```

The installer prefers a GitHub **release tarball** and verifies its **SHA-256** against that release’s `checksums.txt` before extracting into `~/.local/bin`. It falls back to `go install` only if the prebuilt path fails. Review notes: [`docs/security-review-20260805.md`](docs/security-review-20260805.md).

### Install integrity / trust root

Release assets are signed keylessly with **cosign (sigstore)**, bound to this repository's GitHub Actions OIDC identity:

- Signing identity: `https://github.com/ph-commons/pse-edge-pp-cli/.github/workflows/release.yml@refs/tags/v<semver>`
- OIDC issuer: `https://token.actions.githubusercontent.com`

When `cosign` is on the installer's `PATH`, `install.sh` verifies the checksums signature with exactly those constraints (and the release workflow self-verifies the same way before promoting a draft release). The signature is attached per artifact as `<artifact>.sigstore.json` (cosign `--bundle` form). Verification needs **cosign ≥ 2.4.2** (bundle auto-detection); a signature that fails verification is always fatal — never silently downgraded.

What this protects and does not:

- **Protects:** transport/CDN tampering and any asset that was not produced by the release pipeline (a SHA-256-only check cannot detect a compromise that regenerated matching checksums). Signatures bind assets to the OIDC identity of the actual `release.yml` workflow run.
- **Caveat:** an active attacker who can also intercept/block the signature fetch (not just tamper with the assets) can force the checksum-only fallback. This is closed by setting `PSE_EDGE_REQUIRE_COSIGN=1`, which makes any fallback a hard failure. Default posture protects against passive/incompetent tampering.
- **Does not protect:** a fully compromised maintainer account that can push tags (that account can trigger valid workflow runs and produce valid signatures). Nor does it satisfy macOS Gatekeeper or Windows SmartScreen (no notarization / Authenticode).
- **Transparency:** keyless signing publishes artifact digests to the public [Rekor transparency log](https://www.sigstore.dev/).

Fallbacks and limits:

- If `cosign` is not installed, the installer falls back to SHA-256 checksum-only and prints an explicit warning. Set `PSE_EDGE_REQUIRE_COSIGN=1` to turn this into a hard failure.
- Releases are created by tag push only (`release.yml`); manual (`workflow_dispatch`) releases are intentionally unsupported because their branch identity cannot match the tag identity `install.sh` requires.
- The `curl | bash` fetch of `install.sh` itself remains trust-on-first-use on the raw GitHub URL.

An MCP server binary is also available for IDE/desktop agents:

```bash
go install github.com/ph-commons/pse-edge-pp-cli/cmd/pse-edge-pp-mcp@latest
```

Manual Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "pse-edge": {
      "command": "pse-edge-pp-mcp"
    }
  }
}
```

## Quick Start

```bash
# Health check — verifies both upstreams are reachable; no auth exists anywhere
pse-edge-pp-cli doctor --dry-run

# Is the PH market open, and what is the last completed trading day?
pse-edge-pp-cli session

# EOD quotes with source, as-of date, and stale flags
pse-edge-pp-cli quote AT GTCAP --json

# Build the local registry and price history
pse-edge-pp-cli sync --resources companies,prices --since 30d

# Performance vs the PSEi from the local store
pse-edge-pp-cli drift AT --since 90d --agent

# Pull the year's disclosures into the local index (this is what deadlines joins against)
pse-edge-pp-cli filings AT --from-date 01-01-2026 --json

```

## Unique Features

These capabilities aren't available in any other tool for this API.

### Local history that compounds
- **`history`** — Query daily OHLC/value history for any ticker or the PSEi from the local store — data no free PSE API serves.

  _Reach for this when a question spans more than the current session; every row carries source and as-of trading date. `--json`/`--agent` emit a coverage wrapper (`bars` + `coverage{first,last,gaps}` + `session_last_completed` + `stale` + `sync_required`, plus `calendar_coverage` outside known holiday years) so automation can tell "no data" from "not synced"._

  ```bash
  pse-edge-pp-cli history AT --since 30d --agent
  ```
- **`drift`** — Percent change over a window, absolute and versus the PSEi over the same sessions, with 52-week band position.

  _Answers 'how has this done vs the market' in one call instead of two fetches and hand math._

  ```bash
  pse-edge-pp-cli drift AT --since 90d --agent
  ```
- **`breadth`** — Advance/decline/unchanged and value-traded trend over time from local index snapshots.

  _Breadth direction over weeks is the tape context a single day's number cannot give._

  ```bash
  pse-edge-pp-cli breadth --since 30d --agent
  ```
- **`movers`** — Ranked gainers and losers across the synced universe for a window, with universe size and as-of stated.

  _Weekend tape review in one call; the output names its universe so partial syncs cannot mislead._

  ```bash
  pse-edge-pp-cli movers --since 7d --agent
  ```

### As-of discipline
- **`session`** — Philippine trading-calendar verdict: trading day or not, pre/post-close gate, last completed trading date.

  _Gate every EOD figure on this before trusting it — blank change fields on non-trading days are states, not zeros._

  ```bash
  pse-edge-pp-cli session --json
  ```
- **`stale`** — Per-ticker last-synced trading date versus the last completed trading day, with in-series gaps listed.

  _Run before analytics so a silently lagged sync cannot masquerade as a market move._

  ```bash
  pse-edge-pp-cli stale --json
  ```

### Filing rhythm
- **`deadlines`** — Computed 17-Q/17-A due dates per SRC Rule 68 45/105-day windows, joined to local disclosures: filed, pending, or overdue.

  _Answers 'has the quarterly report landed and when is it due' without a browser search._

  ```bash
  pse-edge-pp-cli deadlines AT --json
  ```

## Recipes

### Morning tape check

```bash
pse-edge-pp-cli market --json --select psei,breadth.advances,breadth.declines,breadth.unchanged,totals.value
```

One bounded call for index level and breadth without the full sector payload.

### Position review with as-of discipline

```bash
pse-edge-pp-cli quote AT GTCAP HTI --agent --select symbol,close,change_pct,as_of,stale,source
```

Narrow the quote payload to the fields an advisor actually cites.

### Quarter-end filing sweep

```bash
pse-edge-pp-cli deadlines AT --json
```

Computed 17-Q due dates joined to actual filings — filed, pending, or overdue.

### Downstream market export (local store)

```bash
pse-edge-pp-cli export eod --from 2025-01-01 --to 2026-07-27 --format jsonl -o eod.jsonl
pse-edge-pp-cli export index --from 2025-01-01 --format jsonl
```

Versioned JSONL contracts for private analytics pipelines (`pse-edge-export-eod-v1` / `pse-edge-export-index-v1`). Prefer this over reading `data.db` directly — see [docs/downstream-integration.md](docs/downstream-integration.md).

### Disclosure sweep with client-side keyword

```bash
pse-edge-pp-cli filings GTCAP --from-date 01-01-2026 --json
```

Lists the year's disclosures and feeds the local index behind deadlines; `--keyword` is matched client-side because the endpoint ignores it.

**Completeness:** a successful `filings` response means `announcements/search.ax` answered — not that every official disclosure is in the list. JSON includes `scanned_pages`, `total_pages`, `total_count`, `complete` (relative to the search result set only), `freshness_gap_days`, and `warnings`. For a known `edge_no` missing from search, use the viewer path:

```bash
pse-edge-pp-cli filings get --edge-no 2bc053ab3b1339fb64d70b69f0a3140b --json
pse-edge-pp-cli filings latest-body GTCAP --json
```

(`disclosures view --edge-no` is the generated raw-HTML path; prefer `filings get` when you need structured company/title/attachment fields.)

Which commands carry `file_id` / `document_file_id`:

| Command | `file_id` / `document_file_id` |
|---|---|
| `filings SYMBOL` | no (index only) |
| `filings get --edge-no` | yes |
| `filings latest-body SYMBOL` | yes + body |
| `disclosures document --file-id` | yes (input + body) |

### Relative strength question

```bash
pse-edge-pp-cli drift AT --since 90d --agent
```

Absolute and vs-PSEi performance with 52-week band position from the local store.

## Usage

Run `pse-edge-pp-cli --help` for the full command reference and flag list.

### Delivering output (`--deliver`)

`--deliver webhook:<url>` POSTs command output to a URL instead of (or in addition to) stdout. For safety, the webhook sink **refuses by default** any destination whose host resolves to a private, link-local, cloud-metadata (`169.254.169.254`), or reserved IP range, and it re-validates every redirect hop. Notes:

- Opt out explicitly with `--deliver-webhook-allow-private` (e.g. to a local service on `127.0.0.1`).
- If the host cannot be resolved, delivery is refused (fail-closed) — a transient resolver outage will block delivery.
- The check is resolve-then-check: it prevents accidental or compromised-argument misrouting, not a DNS-rebinding attacker.
- Delivery through a proxy that performs its own DNS is outside this guard's guarantees.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data such as `data.db` |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `PSE_EDGE_CONFIG_DIR`, `PSE_EDGE_DATA_DIR`, `PSE_EDGE_STATE_DIR`, or `PSE_EDGE_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `PSE_EDGE_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export PSE_EDGE_HOME=/srv/pse-edge
pse-edge-pp-cli doctor
```

Under `PSE_EDGE_HOME=/srv/pse-edge`, the four dirs resolve to `/srv/pse-edge/config`, `/srv/pse-edge/data`, `/srv/pse-edge/state`, and `/srv/pse-edge/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "pse-edge": {
      "command": "pse-edge-pp-mcp",
      "env": {
        "PSE_EDGE_HOME": "/srv/pse-edge"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `PSE_EDGE_DATA_DIR` overrides an explicit `--home` for that kind. Use `PSE_EDGE_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `PSE_EDGE_HOME` does not move files back to platform defaults, and `doctor` cannot find files left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. Run `pse-edge-pp-cli doctor --fail-on warn` to check path warnings in automation.

## Commands

### companies

Listed-company registry: directory, lookup, and profiles

- **`pse-edge-pp-cli companies directory`** - One page of the full listed-company directory (name, symbol, cmpy_id, security_id)
- **`pse-edge-pp-cli companies lookup`** - Search companies by name or symbol prefix (first 20 alphabetical matches only — use exact-match filtering)
- **`pse-edge-pp-cli companies profile`** - Company profile page: sector, subsector, incorporation, auditor

### disclosures

Corporate disclosures: search, view, and read filing documents

- **`pse-edge-pp-cli disclosures document`** - Full disclosure document body for `--file-id` (HTML or PDF; CLI sniffs `%PDF-` magic bytes). JSON `results` is `{file_id, content_type, text, byte_length}`; PDF text layers are extracted with Poppler `pdftotext`. Still uses `downloadHtml.do`, never the broken `downloadFile.do` path.
- **`pse-edge-pp-cli disclosures search`** - Search disclosures by company, template, and date range (server-side; the keyword parameter is IGNORED upstream — use the filings command for client-side keyword filtering). Upstream expects a form-urlencoded body, not JSON.
- **`pse-edge-pp-cli disclosures view`** - Disclosure viewer wrapper for one filing

Read attachment text using only the CLI (use a `file_id` from the disclosure viewer):

```bash
pse-edge-pp-cli disclosures document --file-id 1959980 --json --no-learn
```

The command fetches `downloadHtml.do`, detects HTML or PDF from the bytes, and returns text in `results.text` with `results.content_type`. It never prints PDF bytes. PDF extraction requires Poppler's `pdftotext` on `PATH` (`brew install poppler` on macOS; package `poppler-utils` on Debian/Ubuntu). HTML needs no extractor.

PDF extraction stops after 30 seconds or command cancellation. Missing `pdftotext`, invalid or unreadable PDFs, and PDFs without a text layer exit non-zero with an error. Scanned PDFs need OCR; this command does not perform it. `filings latest-body` uses the same extraction.

### financials

17-Q/17-A financial summary tables (annual + quarterly)

- **`pse-edge-pp-cli financials`** - Balance-sheet and income-statement summary tables for a company (server-rendered HTML; authoritative when the PDF download is broken)

### market

Market-wide data: PSEi, sector indices, breadth, totals

- **`pse-edge-pp-cli market`** - Composite/sector page: PSEi and sector indices, market summary (volume, value, trades, advances/declines/unchanged), embedded daily PSEi series since 2021. HTML-entity-encoded JSON uses single-encoded &quot; — decode before parsing. Page is ~1.5MB.

### prices

Per-ticker prices: current session snapshot and daily history

- **`pse-edge-pp-cli prices history`** - Daily OHLC and value-traded history for one security (first-party JSON)
- **`pse-edge-pp-cli prices snapshot`** - Current-session stock data page: last price, change, OHLC, volume, value, market cap

### quotes

Fast EOD quotes via the community phisix JSON mirror (unofficial redeploy — may vanish; edge endpoints are the first-party fallback)

- **`pse-edge-pp-cli quotes <symbol>`** - EOD quote for one ticker: close, percent change, volume. as_of is a synthetic midnight stamp — it carries no trade-time information; derive the trading date from the session calendar.


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`pse-edge-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`pse-edge-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`pse-edge-pp-cli learnings list`** - Inspect taught rows
- **`pse-edge-pp-cli learnings forget <query>`** - Undo a teach
- **`pse-edge-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`pse-edge-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`pse-edge-pp-cli teach-pattern`** - Install a query/resource template up front
- **`pse-edge-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `PSE_EDGE_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `pse-edge-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
pse-edge-pp-cli disclosures search

# JSON for scripting and agents
pse-edge-pp-cli disclosures search --json

# Filter to specific fields
pse-edge-pp-cli disclosures search --json --select id,name,status

# Dry run — show the request without sending
pse-edge-pp-cli disclosures search --dry-run

# Agent mode — JSON + compact + no prompts in one flag
pse-edge-pp-cli disclosures search --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Explicit retries** - safe to retry any command: every command is a read against public market data
- **Read-only** - this CLI performs no remote writes; the only mutations are to its local SQLite cache
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `5` API error, `7` rate limited, `10` config error.

## Health Check

```bash
pse-edge-pp-cli doctor
```

Verifies configuration and connectivity to the API.

## Configuration

Run `pse-edge-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/github.com/ngpestelos/pse-edge-pp-cli/config.toml`; `--home`, `PSE_EDGE_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

## Troubleshooting
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific
- **quote shows stale: true or blank change fields** — Run pse-edge-pp-cli session — weekends and PH holidays are non-trading sessions; the last completed trading day is the honest as-of
- **Edge endpoints intermittently return 404 or empty shells** — The JBoss backend has outage episodes; quote falls back to the other source automatically — re-run doctor to see per-source status
- **history or drift returns empty for a ticker** — Run pse-edge-pp-cli stale to check sync coverage, then pse-edge-pp-cli sync --resources prices --since 90d
- **filings omits a disclosure visible on the official viewer** — PSE EDGE search is not a complete corpus (issue #10). Check JSON `warnings`, `complete`, and `freshness_gap_days`. Look up a known `edge_no` with `pse-edge-pp-cli filings get --edge-no <hash> --json` or open the viewer URL directly
- **Repeated large fetches feel slow** — The market page is ~1.5MB; results are cached with a TTL — avoid tight polling loops, PSE data is EOD-only

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**pse-data-scraper**](https://github.com/mmangkad/pse-data-scraper) — Python
- [**pse-tracker-cli**](https://github.com/ianvizarra/pse-tracker-cli) — JavaScript
- [**psei-cli**](https://github.com/briancalma/psei-cli) — JavaScript
- [**phisix**](https://github.com/phisix-org/phisix) — Java

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
