# Issue #25 — `--deliver webhook` SSRF guard

Hand patch on top of the Printing Press print. Do not drop on reprint.

## Problem

`--deliver webhook:<url>` accepted any `http(s)` URL and POSTed CLI output
with a 30s timeout — no destination restriction. An agent (or a
compromised/careless arg list) could direct output at internal hosts
(cloud metadata endpoints, localhost services). Documented as an SSRF
surface in `docs/security-review-20260805.md` (M2), owner chose option A
(block by default + explicit opt-in).

## Fix

- `internal/cli/deliver.go`
  - `DeliverSink.AllowPrivate bool` (set only via the new flag).
  - `blockedDestinationRanges []netip.Prefix` — loopback, RFC1918, CGNAT,
    link-local (incl. 169.254.169.254), ULA, multicast, reserved /
    TEST-NET / benchmark, 6to4/Teredo, NAT64 64:ff9b::/96 (RFC 6052)
    and local-use 64:ff9b:1::/48 (RFC 8215).
  - `isBlockedDestinationIP` — `netip.AddrFromSlice` + `Unmap`; unparseable
    fails closed.
  - `checkWebhookDestination` — `url.Parse` → `u.Hostname()` → `net.LookupIP`;
    blocks if ANY resolved IP is blocked; fail-closed on resolution failure.
  - `webhookClient(allowPrivate)` — `CheckRedirect` re-runs the guard on
    every redirect hop (closes the redirect-bypass SSRF).
  - `deliverWebhook` now takes `allowPrivate` and uses `webhookClient`.
- `internal/cli/root.go` — `--deliver-webhook-allow-private` persistent flag
  (default false); help text updated.
- `internal/cli/profile.go` — `--deliver` and `--deliver-webhook-allow-private`
  in `reservedProfileFlags` and `profileSaveSkipFlags` (never overlay, never
  capture). `profile save` Long names those flags.
- `internal/cli/profile_guard_test.go` — overlay refusal + skip-map superset.
- `internal/mcp/cobratree/shellout.go` — `deliver-webhook-allow-private`
  added to `blockedRootFlags` (defense-in-depth; `deliver` already blocked).
- `internal/mcp/cobratree/shellout_test.go` — `TestCliArgsFromMCP_BlocksRootFlags`
  passes the production `blockedRootFlags` map (plus `reservedStructuredArgs`).
- `internal/cli/deliver_test.go` — new white-box tests: IP classification
  table (blocked/pass/fail-closed), `checkWebhookDestination` literals +
  localhost + hex-form/trailing-dot, redirect policy, AllowPrivate opt-in
  via loopback httptest servers.

## Files

- `internal/cli/deliver.go`
- `internal/cli/deliver_test.go`
- `internal/cli/root.go`
- `internal/cli/profile.go`
- `internal/cli/profile_guard_test.go`
- `internal/mcp/cobratree/shellout.go`
- `internal/mcp/cobratree/shellout_test.go`
- `README.md` (deliver section), `CHANGELOG.md` (Unreleased/Security)

## Accepted limits (documented in code + README)

- Resolve-then-check: mitigates accidental/compromised-arg misrouting, not
  DNS rebinding.
- Fail-closed on DNS failure: transient resolver outage blocks delivery.
- Proxy-mediated delivery is outside the guard's guarantees.

## Verify

```bash
go test ./...
go test -race ./internal/cli/ ./internal/mcp/cobratree/
go vet ./...
```

## Changelog

Entry under `CHANGELOG.md` → `## [Unreleased]` / Security (policy:
hand-maintained for this independent repo as of 2026-08-04).
