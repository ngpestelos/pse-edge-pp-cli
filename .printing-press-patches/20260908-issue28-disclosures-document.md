# Issue #28 — disclosures document body (PDF vs empty page extract)

Hand patch on top of the Printing Press print. Do not drop on reprint.

## Problem

`disclosures document --file-id` GETs `/downloadHtml.do` (the working path)
then runs `extractHTMLResponse` in page mode. A PDF body (`%PDF-…`) or HTML
without og:title/links marshals to `{}` via `htmlExtractedPage` omitempty.
`wrapWithProvenance` wraps that as `{ "meta": { "source": "live" }, "results": {} }`
with exit 0. Auto live reads also `writeThroughCache` as resourceType
`disclosures`, which warns "no extractable ID field".

## Fix

- `decodeDisclosureDocument(fileID, raw)` sniffs `%PDF-` vs HTML and returns
  `{file_id, content_type, text, byte_length}`. Empty / unusable bodies
  return a non-zero `notFoundErr`.
- `internal/cli/disclosures_document.go` fetches with `c.GetWithHeaders`
  (no write-through-cache as disclosures search rows) and decodes instead of
  page-meta extract. HTTP errors still go through `classifyAPIError`.
- JSON `results` is the document object, never `{}`.
- No PDF library. PDF `text` may be empty when magic matches and bytes > 0.

## Files

- `internal/cli/disclosure_document_body.go`
- `internal/cli/disclosure_document_body_test.go`
- `internal/cli/disclosures_document.go`
- `README.md`, `CHANGELOG.md` (Unreleased/Fixed)

## Verify

```bash
go test ./internal/cli/ -run 'TestDecodeDisclosureDocument|TestDisclosuresDocument'
go test ./...
go vet ./...
```

## Changelog

Entry under `CHANGELOG.md` → `## [Unreleased]` / Fixed (policy: hand-maintained
for this independent repo as of 2026-08-04).
