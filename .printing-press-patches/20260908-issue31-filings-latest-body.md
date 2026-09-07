# Issue #31 — filings latest-body one-shot (file_id + body)

Hand patch on top of the Printing Press print. Do not drop on reprint.

## Problem

Agents needed the latest filing's `file_id` and document body for a ticker.
Search rows (`filings SYMBOL`) only carry `edge_no`. Callers were scraping
`openDiscViewer.do` or chaining `filings get` by hand.

## Fix

- Keep `filings SYMBOL` as an index (no `file_id` on every search row).
- Add `filings latest-body SYMBOL`: page-1 search (date DESC) →
  `openDiscViewer.do` → `document_file_id` else first attachment `file_id` →
  `downloadHtml.do` via existing `decodeDisclosureDocument`.
- `FetchDisclosurePage` and `FetchDisclosureViewer` honor `PSE_EDGE_BASE_URL`
  (trim trailing slash + path). Production default unchanged.

## Files

- `internal/cli/filings_latest_body.go`
- `internal/cli/filings_latest_body_test.go`
- `internal/cli/disclosures_filings.go`
- `internal/pseedge/disclosures.go`
- `internal/pseedge/disclosure_viewer.go`
- `README.md`, `CHANGELOG.md` (Unreleased/Added), `SKILL.md`

## Verify

```
go test ./...
go vet ./...
```
