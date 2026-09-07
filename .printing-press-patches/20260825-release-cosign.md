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
    cosign `--bundle` form: `signature: "${artifact}.sigstore.json"`, args
    `[sign-blob, --bundle=${signature}, ${artifact}, --yes]`. cosign
    deprecated `--output-signature`/`--output-certificate` in favor of
    `--bundle`; the bundle form is the current goreleaser-documented
    pattern and works on both cosign v2 (≥2.4.2) and v3.
- `.github/workflows/release.yml`
  - `id-token: write` permission (OIDC identity for keyless signing).
  - `sigstore/cosign-installer` pinned to the v3.9.2 commit SHA
    (`d58896d6a1865668819e1d91763c7751a165e159`) — trust-critical action.
  - `goreleaser/goreleaser-action` pinned to the v7.2.3 commit SHA
    (`f06c13b6b1a9625abc9e6e439d9c05a8f2190e94`) — it invokes cosign under
    OIDC and holds GITHUB_TOKEN.
  - **Draft-gated publish:** goreleaser runs `release --clean --draft`;
    the post-build self-check verifies the produced checksums signature
    with the exact identity/issuer `install.sh` uses; the draft is promoted
    (`gh release edit --draft=false`) only after verification passes. A
    verify failure leaves the release unpublished.
  - **Tag push only:** `workflow_dispatch` removed. A manual run is signed
    with a `refs/heads/<branch>` identity that install.sh must reject
    (fail-closed), so manual releases are intentionally unsupported.
- `scripts/install.sh`
  - `verify_checksums_signature`: when `cosign` is on PATH, fetch
    `checksums.txt.sigstore.json` and `cosign verify-blob --bundle ...`
    with `--certificate-identity-regexp
    'https://github.com/ph-commons/pse-edge-pp-cli/.github/workflows/release.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'`
    (semver-tight) and `--certificate-oidc-issuer
    'https://token.actions.githubusercontent.com'`; dies on verification
    failure (never silently downgraded). Needs cosign ≥ 2.4.2.
  - Fallbacks with distinct warnings: cosign absent → checksum-only;
    signature unavailable → checksum-only. `PSE_EDGE_REQUIRE_COSIGN=1`
    turns either fallback into a hard failure.
  - Runs BEFORE the SHA-256 tarball check (refuse tampered/wrong-identity
    before even checksum-matching).
- `README.md` — "Install integrity / trust root" section: signing identity
  (semver regexp), issuer, what keyless signing protects / does not
  (compromised maintainer account; no Gatekeeper/SmartScreen), the active
  attacker caveat (suppressed signature fetch → fallback unless
  `PSE_EDGE_REQUIRE_COSIGN=1`), public Rekor tlog disclosure, min cosign
  version, tag-only releases, `curl|bash` TOFU note.
- `docs/security-review-20260805.md` — H1 residual "No cosign/sigstore
  provenance" marked resolved.
- `CHANGELOG.md` — Unreleased / Security.

## Files

- `.goreleaser.yaml`
- `.github/workflows/release.yml`
- `scripts/install.sh`
- `README.md`
- `docs/security-review-20260805.md` (residual note)
- `CHANGELOG.md`

## Accepted limits (documented in README)

- Keyless signing does not protect a fully compromised maintainer account
  (can trigger valid workflow runs).
- An active attacker who can also suppress the signature fetch can force the
  checksum-only fallback; closed by `PSE_EDGE_REQUIRE_COSIGN=1`.
- Releases are tag push only (manual releases intentionally unsupported).
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
