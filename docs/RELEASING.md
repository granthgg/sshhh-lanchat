# Releasing

## Cutting a release

```sh
# 1. bump `const version` in cmd/lanchat/main.go and commit
# 2. tag and push — CI does the rest
git tag v2.6.1
git push origin v2.6.1
```

The [release workflow](../.github/workflows/release.yml) cross-compiles all six
desktop targets, writes `checksums.txt` (SHA-256 of every asset), and publishes
a GitHub Release whose notes lead with the one-command installers.

## How installs stay warning-free today

The binaries are **not code-signed**, so a *browser-downloaded* copy triggers
Windows SmartScreen ("unknown publisher") and macOS Gatekeeper ("unverified
developer"). Those warnings are keyed to the *download path*, not the file
contents: browsers stamp downloads with a mark (mark-of-the-web on Windows,
`com.apple.quarantine` on macOS) that tells the OS to screen them.

The web installers ([get.ps1](../scripts/get.ps1), [get.sh](../scripts/get.sh))
therefore:

1. download with `Invoke-WebRequest` / `curl` instead of a browser,
2. **verify the SHA-256 against the release's `checksums.txt`** — this is the
   integrity guarantee a signature would otherwise give,
3. clear any download mark (`Unblock-File` — the same thing Scoop does), and
4. install onto the user's PATH.

Result: no popup, and stronger integrity assurance than a click-through.

## Removing the warnings permanently (the real fix)

Signing attaches a verified publisher identity to the file itself, so even
browser downloads stop warning. Options, cheapest-first:

### Windows — Authenticode signing

| Option | Cost | Notes |
|---|---|---|
| **SignPath.io Foundation** | free | Free code-signing for open-source projects; integrates with GitHub Actions. Apply at signpath.org. |
| **Azure Trusted Signing** | ~$10/month | Microsoft's hosted signing; open to individuals with 3+ years of verifiable identity history (supported regions). Has a GitHub Action. |
| **OV certificate** | ~$100–400/yr | Signs immediately, but SmartScreen reputation still builds over weeks of downloads. |
| **EV certificate** | ~$300–500/yr | Immediate SmartScreen reputation; requires a registered business. |

Once you have any of these, add a signing step to `release.yml` between
`make cross` and the checksum step (each provider documents its Action).

Also useful regardless of signing:

- **Defender false positives**: Go binaries occasionally trip heuristics.
  Submit the file at <https://www.microsoft.com/en-us/wdsi/filesubmission> as a
  false positive — Microsoft typically clears it within days, for everyone.
- **winget**: after signing, submit a manifest to
  [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) so
  `winget install lanchat` works with no warnings at all.
- **Scoop**: a bucket manifest works fine even unsigned (Scoop downloads via
  CLI, so no mark-of-the-web).

### macOS — Developer ID + notarization

1. Join the Apple Developer Program ($99/yr) and create a **Developer ID
   Application** certificate.
2. In CI: `codesign --sign "Developer ID Application: …" --options runtime`,
   then `xcrun notarytool submit --wait`, then `xcrun stapler staple`.
   (Needs a macOS runner for the binaries' signing/stapling step.)
3. After that, even browser-downloaded binaries pass Gatekeeper.

Free alternative that most Mac CLI users prefer anyway: a **Homebrew tap**
(`brew install granthgg/tap/lanchat`). Create a `homebrew-tap` repository with
a formula pointing at the release tarball + its SHA-256 — Homebrew downloads
aren't quarantined, so there's no Gatekeeper prompt and no cert needed.

Note: Go's linker already ad-hoc-signs `darwin/arm64` binaries during
cross-compilation (Apple Silicon requires at least an ad-hoc signature to
execute), which is why the current builds run at all once quarantine is
cleared. Ad-hoc signatures carry no identity, so they don't satisfy Gatekeeper
for downloaded files — only real Developer ID signing plus notarization does.
