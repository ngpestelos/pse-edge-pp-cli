# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

**Policy (2026-08-04):** Hand-maintained. Every user-facing fix/feature PR updates
`## [Unreleased]`. On tag release, move those bullets under a new version heading
and clear Unreleased. Do **not** leave changelog work to printing-press-library
automation for this independent repo (that rule applies only when publishing
*into* the library catalog).

## [Unreleased]

### Fixed

- `history` and `export eod` emit `volume_status` (`ok` or `unavailable`) so a missing share volume is never confused with a genuine zero. Null `volume` is now explicit on history JSON. Contract id stays `pse-edge-export-eod-v1` ([#27](https://github.com/ph-commons/pse-edge-pp-cli/issues/27)).
- `disclosures document --file-id` now returns a structured body (`file_id`, `content_type`, `text`, `byte_length`) for HTML and PDF attachments instead of `{ "results": {} }` with exit 0. PDF is sniffed from `%PDF-` magic bytes; empty or unusable bodies exit non-zero ([#28](https://github.com/ph-commons/pse-edge-pp-cli/issues/28)).
- Data race in the learn loop's query-synonym registry (`RegisterQuerySynonyms`) that could crash concurrent installs with `fatal error: concurrent map writes`. Registration and reads are now guarded by a package-level `sync.RWMutex`, with a pinned `-race` regression test. CI now runs `go test -race ./...`.

### Security

- `--deliver webhook:<url>` now refuses destinations that resolve to private / link-local / cloud-metadata / reserved IP ranges (SSRF guard, issue #25), including NAT64 well-known (`64:ff9b::/96`) and local-use (`64:ff9b:1::/48`) prefixes. The guard also re-validates every redirect hop. Opt out explicitly with `--deliver-webhook-allow-private`. DNS resolution failure blocks delivery (fail-closed); the check is resolve-then-check and does not defend against DNS rebinding. The flag is blocked from the MCP tool surface alongside `--deliver`.

## [0.1.5] - 2026-08-18

### Changed

- Move repo and Go module to [`ph-commons/pse-edge-pp-cli`](https://github.com/ph-commons/pse-edge-pp-cli) (User-Agent strings unchanged).

## [0.1.4] - 2026-08-18

### Security

- Require Go 1.26.6 (stdlib vulns GO-2026-6090 / 6089 / 5972 / 5026 on 1.26.5). Pin CI and release `setup-go` to `1.26.6` so Dependabot PRs can pass `govulncheck` ([#22](https://github.com/ngpestelos/pse-edge-pp-cli/pull/22)).

### Changed

- Bump `github.com/mark3labs/mcp-go` 0.57.0 → 0.58.0 ([#20](https://github.com/ngpestelos/pse-edge-pp-cli/pull/20)).
- Bump `golang.org/x/net` 0.57.0 → 0.58.0 ([#21](https://github.com/ngpestelos/pse-edge-pp-cli/pull/21)).
- Bump `modernc.org/sqlite` 1.55.0 → 1.56.0 ([#19](https://github.com/ngpestelos/pse-edge-pp-cli/pull/19)).

## [0.1.3] - 2026-08-05

### Added

- `filings get --edge-no` — direct `openDiscViewer.do` lookup when search omits a known disclosure ([#10](https://github.com/ngpestelos/pse-edge-pp-cli/issues/10), [#15](https://github.com/ngpestelos/pse-edge-pp-cli/pull/15)).
- Local-store export for downstream pipelines: `export eod`, `export index`, `export companies-local` with versioned per-row `contract` ids (`pse-edge-export-*-v1`) ([#9](https://github.com/ngpestelos/pse-edge-pp-cli/issues/9), [#18](https://github.com/ngpestelos/pse-edge-pp-cli/pull/18)).
- Downstream integration guide: `docs/downstream-integration.md` ([#9](https://github.com/ngpestelos/pse-edge-pp-cli/issues/9)).
- Security review notes: `docs/security-review-20260805.md` ([#13](https://github.com/ngpestelos/pse-edge-pp-cli/issues/13), [#17](https://github.com/ngpestelos/pse-edge-pp-cli/pull/17)).
- `which` indexes `filings` and `export` for capability routing ([#16](https://github.com/ngpestelos/pse-edge-pp-cli/pull/16)).

### Changed

- `filings` JSON exposes search honesty fields: `complete` (search set only), `truncated`, `page_cap_hit`, `freshness_gap_days`, standing `warnings` that `announcements/search.ax` is not an authoritative complete corpus ([#10](https://github.com/ngpestelos/pse-edge-pp-cli/issues/10)).
- MCP HTTP default bind is loopback-only (`127.0.0.1:7777`); warn on non-loopback binds (no auth) ([#13](https://github.com/ngpestelos/pse-edge-pp-cli/issues/13)).

### Security

- `scripts/install.sh` verifies prebuilt release tarballs against the release `checksums.txt` (SHA-256) before extract; refuse install on missing entry or mismatch ([#13](https://github.com/ngpestelos/pse-edge-pp-cli/issues/13)).
- `filings` / `filings get` HTTP clients set an explicit 60s timeout ([#13](https://github.com/ngpestelos/pse-edge-pp-cli/issues/13)).
- SQLite DSN builder rejects path URI metacharacters that could override `mode=ro`; MCP blocks `--db` ([#13](https://github.com/ngpestelos/pse-edge-pp-cli/issues/13)).
- Cross-host HTTP redirects drop `Config.Headers` keys, not only `Authorization` ([#13](https://github.com/ngpestelos/pse-edge-pp-cli/issues/13)).

## [0.1.2] - 2026-08-04

### Fixed

- Parse PSE Edge `stockData.do` down-day `Change(% Change)` cells that use U+00A0 interior percent whitespace; require `up`/`down` prefix so an unmatched direction word cannot silently invert sign ([#8](https://github.com/ngpestelos/pse-edge-pp-cli/issues/8),
  [#14](https://github.com/ngpestelos/pse-edge-pp-cli/pull/14)).

### Changed

- Bump `golang.org/x/net` 0.55.0 → 0.57.0 ([#12](https://github.com/ngpestelos/pse-edge-pp-cli/pull/12)).

## [0.1.1] - 2026-07-27

### Changed

- Five-step densify parallel quote path; `resolveVersion` for non-dev installs (see release notes / git history).

## [0.1.0] - 2026-07-27

### Added

- Initial public release: agent-native PSE Edge CLI (quotes, filings, local
  history, MCP).

[Unreleased]: https://github.com/ph-commons/pse-edge-pp-cli/compare/v0.1.5...HEAD
[0.1.5]: https://github.com/ph-commons/pse-edge-pp-cli/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/ph-commons/pse-edge-pp-cli/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/ngpestelos/pse-edge-pp-cli/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/ngpestelos/pse-edge-pp-cli/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/ngpestelos/pse-edge-pp-cli/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/ngpestelos/pse-edge-pp-cli/releases/tag/v0.1.0
