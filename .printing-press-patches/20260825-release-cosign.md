# Issue #26 — cosign keyless provenance for release assets

Hand patch on top of the Printing Press print. Do not drop on reprint.

## Problem

`scripts/install.sh` verified release tarballs against `checksums.txt`
(SHA-256), but the checksums file was unsigned — an attacker able to
publish a malicious release (compromised release pipeline or maintainer
account) could regenerate matching checksums. SHA-256 verifies transport
integrity, not provenance. Follow-up from `docs/security-review-20260805.md`
(H1 residual).

## Fix

- `.goreleaser.yaml`
  - `sboms:` — per-archive SPDX SBOM via syft (goreleaser v2 default args,
    `spdx-json=$document` so a real file is produced).
  - `signs:` — three keyless cosign blocks (checksum / archive / sbom) in
    **cosign v3 `--bundle` form**: `signature: "${artifact}.sigstore.json"`,
    args `[sign-blob, --bundle=${signature}, ${artifact}, --yes]`. (cosign v3
    removed `--output-signature`/`--output-certificate`; the bundle form is
    the current goreleaser-documented pattern.)
- `.github/workflows/release.yml`
  - `id-token: write` permission (OIDC identity for keyless signing).
  - `sigstore/cosign-installer` pinned to the v3.9.2 commit SHA
    (`d58896d6a1865668819e1d91763c7751a165e159`) — trust-critical action.
  - Post-goreleaser self-check: `cosign verify-blob --bundle` on the produced
    checksums signature with the exact identity/issuer `install.sh` uses, so
    a typo or identity drift fails the release instead of shipping
    unverifiable assets.
- `scripts/install.sh`
  - `verify_checksums_signature`: when `cosign` is on PATH, fetch
    `checksums.txt.sigstore.json` and `cosign verify-blob --bundle ...`
    with `--certificate-identity-regexp
    'https://github.com/ph-commons/pse-edge-pp-cli/.github/workflows/release.yml@refs/tags/v[0-9]+(\.[0-9]+)*.*'`
    and `--certificate-oidc-issuer 'https://token.actions.githubusercontent.com'`;
    dies on verification failure.
  - Fallbacks with distinct warnings: cosign absent → checksum-only;
    signature unavailable (manual/unsigned release vs transient fetch) →
    checksum-only. `PSE_EDGE_REQUIRE_COSIGN=1` turns either fallback into a
    hard failure.
  - Runs BEFORE the SHA-256 tarball check (refuse tampered/wrong-identity
    before even checksum-matching).
- `README.md` — "Install integrity / trust root" section: signing identity,
  issuer, what keyless signing protects / does not (compromised maintainer
  account; no Gatekeeper/SmartScreen), public Rekor tlog disclosure, fallback
  semantics + `PSE_EDGE_REQUIRE_COSIGN=1`, `curl|bash` TOFU note.
- `CHANGELOG.md` — Unreleased / Security.

## Files

- `.goreleaser.yaml`
- `.github/workflows/release.yml`
- `scripts/install.sh`
- `README.md`
- `CHANGELOG.md`

## Accepted limits (documented in README)

- Keyless signing does not protect a fully compromised maintainer account
  (can trigger valid workflow runs).
- Manual (`workflow_dispatch`) releases run at `refs/heads/<branch>` and do
  not match the tag identity → checksum-only fallback.
- Full keyless signing path only exercises on a real tag release in CI;
  local validation covers `goreleaser check` + verify-blob flag correctness.

## Verify

```bash
goreleaser check -f .goreleaser.yaml
bash -n scripts/install.sh
go build ./... && go test ./...
```

## Changelog

Entry under `CHANGELOG.md` → `## [Unreleased]` / Security (policy:
hand-maintained for this independent repo as of 2026-08-04).
