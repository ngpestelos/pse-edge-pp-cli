# Security review — pse-edge-pp-cli (issue #13)

**Date:** 20260805 (Asia/Manila)  
**Scope:** CLI surface, upstream HTTP clients, local SQLite store, `scripts/install.sh`, MCP binary  
**Method:** Code review + `govulncheck ./...` + live release asset checksum probe  
**Verdict:** No CRITICAL findings. One HIGH install integrity gap **fixed** in the same PR as this note. Residual MEDIUM/LOW tracked below.

## Executive summary

| Severity | Count | Disposition |
|----------|------:|-------------|
| CRITICAL | 1 | Fixed: MCP HTTP default bind loopback-only (`127.0.0.1:7777`) + non-loopback warning |
| HIGH | 3 | Fixed: install SHA-256; SQLite DSN path injection; MCP blocks `--db` |
| MEDIUM | several | Filings timeouts fixed; webhook SSRF / rate-limit defaults documented residual |
| LOW / INFO | several | Documented residual |

`govulncheck ./...` (20260805): **No vulnerabilities found.**

Threat model: personal/agent workstation CLI for public PSE market HTML/JSON. No user credentials for PSE EDGE. Blast radius is mainly **local integrity** (what binary gets installed) and **agent-driven side effects** (`--deliver`, local DB, MCP shell-out).

---

## 1. Supply chain / install


### C1 — MCP streamable HTTP bound all interfaces without auth — **FIXED**

**Before:** `cmd/pse-edge-pp-mcp` default `--addr :7777` (all interfaces), no bearer/mTLS.

**Fix:** Default `127.0.0.1:7777`; warn loudly if bind is non-loopback (still no auth — prefer stdio).

### H2 — SQLite DSN path URI injection — **FIXED**

**Before:** `file:` + raw `dbPath` + `?mode=ro&…` allowed a path containing `?mode=rwc` to keep the first `mode=` and defeat read-only.

**Fix:** `validateDBPath` rejects `?#&` and `file:` prefix; `sqliteDSN` builds escaped `file:` URIs; `CleanPathOverride` rejects the same metacharacters.

### H3 — MCP shell-out allowed `--db` — **FIXED**

**Before:** `blockedRootFlags` omitted `db`, so MCP could retarget the store path (and combine with H2).

**Fix:** `db` added to `blockedRootFlags` (+ tests).

### H1 — Prebuilt install did not verify release checksums — **FIXED**

**Before:** `scripts/install.sh` downloaded `pse-edge-pp-cli_${ver}_${os}_${arch}.tar.gz` from GitHub Releases and extracted into `~/.local/bin` without checking `checksums.txt` (which releases already publish).

**Risk:** Compromised CDN/GitHub asset rewrite or MITM after TLS termination could substitute a binary. curl uses TLS (`-fsSL`), so pure network MITM is hard; residual is supply-chain / account compromise class.

**Fix:** Installer now:

1. Downloads tarball **and** `checksums.txt` from the same release tag  
2. Requires an entry for the exact tarball name  
3. Compares local SHA-256 (`shasum -a 256` / `sha256sum`) and **dies** on mismatch or missing entry  

**Not fixed (accepted residual):**

- ~~No cosign/sigstore provenance (would need release pipeline change)~~ — **Resolved 20260825** by issue #26 (keyless cosign signing + installer signature verification; see README "Install integrity").  
- `go install @latest` fallback still trusts the Go module proxy + sumdb (standard Go trust model)  
- `curl | bash` of install.sh itself is still TOFU on the raw GitHub URL (document in README)

### I1 — `go install` / module deps

- No `replace` directives in `go.mod`  
- Direct deps: cobra, mcp-go, modernc.org/sqlite, x/net, pelletier/toml — `govulncheck` clean at review time  
- **Action:** re-run `govulncheck` in CI periodically (recommend follow-up)

---

## 2. Network client

### Strengths

- Main client (`internal/client/client.go`): timeout, transport clone (keeps proxy env), redirect cap (10), cross-host auth header strip, adaptive rate limiting  
- Disclosure fetchers: body `LimitReader` caps; form-urlencoded search; fixed host `edge.pse.com.ph`  
- `filings get --edge-no`: hex allowlist + QueryEscape (PR #15)  
- Dual-source quotes hit known hosts only  

### M1 — Bare `&http.Client{}` on filings path — **partial fix**

`internal/cli/disclosures_filings.go` used clients with no `Timeout` (relied only on `boundCtx`). Hung upstream could strand if a call site omitted a deadline.

**Fix:** `Timeout: 60 * time.Second` on both search and viewer clients.

### M2 — `--deliver webhook:<url>` is intentional SSRF by design

`ParseDeliverSink` allows any `http://` or `https://` URL; POSTs CLI output with 30s timeout. An agent or compromised arg list can POST market JSON to internal hosts (metadata services, localhost).

**Remediation options (not implemented — product choice):**

- Allowlist schemes + block link-local / metadata IPs  
- Require `--yes` confirmation for private ranges  
- Document as operator-controlled sink (current behavior)

**Owner:** follow-up issue if agents run untrusted tool args in multi-tenant hosts.

### L1 — User-Agent identifies the tool

`github.com/ngpestelos/pse-edge-pp-cli/...` — fine for ToS transparency; enables upstream blocking.

### I2 — Rate limiting

Adaptive limiter on main client + disclosure limiter; MCP defaults rate 2 rps. Good neighbor behavior.

---

## 3. Local state

### Strengths

- DB dir `MkdirAll(..., 0o700)`  
- `hardenSQLiteFiles` → `chmod 0o600` on db/wal/shm (best-effort)  
- FTS query tokens extracted via regex then double-quoted — not raw user FTS operators  
- Dynamic SQL table names go through `"` escaping / known-table patterns in store helpers  

### M3 — Custom `--db` / `--home` paths are trusted

Operator can point the store at any path the process can write. Expected for single-user CLI; multi-user hosts should not share a writable home.

### L2 — No secrets in PSE flow

Public data only. Config may hold optional headers; treat config dir as private.

### L3 — Cache under cache dir

HTTP cache files are response bodies; not credentials. Still host-local.

---

## 4. CLI / MCP attack surface

### Strengths (MCP)

- `blockedRootFlags`: `base-url`, `token`, `config`, `deliver`, `home`, `insecure`, `profile`, `client` cannot be set via MCP structured args (`internal/mcp/cobratree/shellout.go`)  
- Shell-out uses argv array (`os/exec`), not shell string join  
- Output capture size-capped (`shelloutCaptureLimit`)  
- SQL tool: `validateReadOnlyQuery` strips comments/noise, allowlists SELECT/WITH, rejects multi-statement; pairs with `mode=ro` (notes VACUUM INTO / ATTACH residual at SQLite layer — gate is load-bearing)

### M4 — MCP still invokes live network for open-world tools

By design (`OpenWorldHintAnnotation(true)`). Hosts must not auto-approve open-world tools without user policy.

### L4 — Dry-run

`--dry-run` short-circuits many commands before network; doctor/session still probe as health tools. Expected.

### I3 — Generated code volume

Much of MCP/CLI is Printing Press generated. Security properties depend on generator stay current; hand-authored paths (`filings`, store harden, install) need review on change.

---

## 5. Trust & docs

- README already states unofficial / not PSE-affiliated  
- Recommend: one-line install integrity note next to `curl | bash` (done in README with this PR)  
- No auth product — no credential storage for EDGE  

---

## Acceptance checklist (issue #13)

- [x] Review notes in-repo (`docs/security-review-20260805.md`) + linked from issue  
- [x] CRITICAL MCP HTTP default bind fixed (loopback)
- [x] HIGH install checksum gap fixed
- [x] HIGH SQLite DSN path injection fixed
- [x] HIGH MCP `--db` blocked  
- [x] Install script + MCP paths covered explicitly  
- [x] MEDIUM residuals documented with owners / product choice  
- [x] Optional follow-ups filed as separate issues (20260825):
  1. CI job: `govulncheck ./...` on PR — [#24](https://github.com/ph-commons/pse-edge-pp-cli/issues/24)
  2. Webhook deliver private-IP policy — [#25](https://github.com/ph-commons/pse-edge-pp-cli/issues/25)
  3. Cosign/sigstore on release assets — [#26](https://github.com/ph-commons/pse-edge-pp-cli/issues/26)  

---

## Evidence commands

```bash
govulncheck ./...
bash -n scripts/install.sh
# checksums exist on release:
curl -fsSL https://github.com/ngpestelos/pse-edge-pp-cli/releases/download/v0.1.2/checksums.txt
```
