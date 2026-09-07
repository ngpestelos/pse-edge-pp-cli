# Downstream integration contract

**Issue:** [#9](https://github.com/ngpestelos/pse-edge-pp-cli/issues/9)  
**Date:** 20260805  

This CLI is an **upstream price/index acquisition layer**. Downstream analytics should treat the **CLI export surface** as the boundary, not the on-disk SQLite layout.

## 1. Is `data.db` a public contract?

**No — not as a binary/schema ABI.**

The four PSE tables (`pse_companies`, `pse_eod_prices`, `pse_index_snapshots`, `pse_disclosures`) are a **build-context red-team contract for this repo’s own commands** (history, drift, breadth, movers, deadlines). They are created lazily with `CREATE TABLE IF NOT EXISTS` and deliberately kept out of the generated `migrate()` / `StoreSchemaVersion` path so older binaries ignore them.

**Downstream should:**

| Prefer | Avoid |
|--------|--------|
| `export eod` / `export index` / `export companies-local` | Opening `data.db` and assuming column set forever |
| `history`, `breadth`, `movers`, `stale` CLIs | Ad-hoc SQL across private helper tables |
| Documented JSON field names + `contract` key | Depending on SQLite type affinities / indexes |

**If you open SQLite anyway:** treat it as a cache that this tool may reshape. Pin a CLI version; re-export after upgrades.

## 2. `export` market resources (supported)

```bash
# Daily equity bars (local store)
pse-edge-pp-cli export eod --from 2025-01-01 --to 2026-07-27 --format jsonl -o eod.jsonl

# Index snapshots + breadth fields when present
pse-edge-pp-cli export index --from 2025-01-01 --format jsonl -o index.jsonl

# Local company registry
pse-edge-pp-cli export companies-local --format jsonl

# Optional filters
pse-edge-pp-cli export eod --symbols AT,GTCAP --from 2026-01-01 --format jsonl
pse-edge-pp-cli export index --codes PSEI --from 2026-01-01 --format jsonl
```

### Versioned row contracts

Every local export row includes `"contract": "<id>"`:

| Resource | Contract ID | Core fields |
|----------|-------------|-------------|
| `eod` | `pse-edge-export-eod-v1` | symbol, trading_date, open, high, low, close, value, volume (nullable), volume_status (`ok` or `unavailable`), source |
| `index` | `pse-edge-export-index-v1` | index_code, trading_date, value, change, pct_change, advances, declines, unchanged, total_volume, total_value, total_trades (nullables), source |
| `companies-local` | `pse-edge-export-companies-v1` | cmpy_id, security_id, symbol, name, etf, synced_at |

**Stability rule:** removing/renaming a field requires a new contract id (`…-v2`). Additive nullable fields may land on the same version with a CHANGELOG note.

Live `export companies` remains the **network** directory scrape (generated path), not the local registry.

## 3. Strict mode for `sync market`

**Today:** hard-fail on some aggregate failures; per-symbol problems emit `sync_warning` events and continue.

**Not implemented in this issue:** `--fail-on-partial`, `--minimum-coverage`, `--manifest out.json`.

**Workaround:** parse stderr/JSON event stream from sync; use `stale --json` and `export eod` row counts as coverage checks. Since #32, `history --json` also exposes machine-readable `coverage{first,last,gaps}` + `session_last_completed` + `stale` + `sync_required` inline. File a follow-up if you still need exit-code gates in CI.

## 4. Volume on `pse_eod_prices`

- Column is `REAL NULL` because **DisclosureCht.ax serves peso value, not share volume** for history bars.
- Session **quote/snapshot** paths have volume; that is **not** currently back-filled into historical EOD rows on each sync.
- Export and `history` surface `"volume": null` when unknown — do not impute zeros.
- Every EOD and `history` row includes `volume_status`: `ok` when `volume` is non-null (including `0`), `unavailable` when `volume` is null. Index history has no share volume, so status is always `unavailable`. Contract id stays `pse-edge-export-eod-v1` (additive field).

## 5. Sector / subsector on registry

- **Not** on `pse_companies` (intentionally lean registry for ID resolution).
- **Available live:** `company AT --json` / `companies profile` (sector, subsector, …).
- **No plan** in-tree today to denormalize sector onto every registry row; join downstream via occasional profile pull or your own sector map.

## 6. Corporate actions / price adjustment

- **History is raw / unadjusted** as returned by PSE Edge `DisclosureCht.ax`.
- No corporate-action table; no split/dividend adjustment layer.
- Discontinuities across splits/stock dividends/rights **will** distort returns and indicators if you treat the series as split-adjusted.
- Disclosures headers (`filings` → `pse_disclosures`) can be a **detection signal**, not a complete CA feed.
- **Documented stance:** downstreams must apply their own adjustment or accept unadjusted analytics.

## 7. Foreign flows

- **Not** in current tables or sync paths.
- Not known to be available from the same free EDGE endpoints this CLI uses. Source separately (e.g. broker/PSE published reports) if required.

## 8. History depth

- `sync market --since` defaults to **30d** (override freely, e.g. `90d`, `365d`, or longer).
- PSEi embedded series on the composite page has been used for **multi-year** backfill (see market sync backfill path; README mentions depth toward 2021 for PSEi).
- Per-ticker `DisclosureCht.ax` depth is **issuer-dependent**; not exhaustively certified for every symbol.
- Known classes of pain: renames, multi-class shares (wrong `security_id`), long suspensions, thin names with sparse bars.
- Use `stale` + `history` empty notes to detect gaps after sync.

## 9. Maintenance intent

- Personal / agent-native tool under Apache-2.0; **best-effort** maintenance, not a paid SLA.
- Breaking changes to **export contracts** will bump contract ids and CHANGELOG.
- Physical SQLite layout may change without a contract bump if CLI commands keep working.
- CI covers unit tests; data-quality is enforced in-process (`ValidateEOD`, session-date gates) rather than a public data-certification program.
- PSE Edge HTML can change without notice — dual-source quotes and doctor help detect outages.

## Recommended pipeline shape

```text
pse-edge-pp-cli sync market --symbols … --since 90d
pse-edge-pp-cli stale --json          # coverage gate
pse-edge-pp-cli export eod --from … --to … --format jsonl | your-loader
pse-edge-pp-cli export index --from … --format jsonl | your-loader
# your private layer: indicators, CA adjustment, foreign flow joins
```

## Related commands

| Need | Command |
|------|---------|
| Freshness | `session`, `stale` |
| Ad-hoc series | `history`, `drift`, `breadth`, `movers` |
| Filings (search incomplete) | `filings`, `filings get --edge-no` |
| Security posture | `docs/security-review-20260805.md` |
