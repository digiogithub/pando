# Release pipeline

Pushing a `v*` tag builds, signs, notarizes and publishes a full release.
The workflow is [`.github/workflows/release.yml`](../.github/workflows/release.yml);
the signing, notarization and release steps come from the shared composite
actions in [`digiogithub/ci-actions`](https://github.com/digiogithub/ci-actions)
(pinned at `@v1`), so the same signing setup is reusable from other Go and Rust
repositories.

```
tag v1.2.3
   ├── job linux-windows (ubuntu-latest)
   │     bun → web-ui embedded assets
   │     wails → desktop wrapper → go:embed
   │     make release-linux-amd64 / release-linux-arm64 / release-windows-amd64
   │        (zig cc as the cross C toolchain, UPX on non-darwin)
   │
   ├── job macos (macos-latest, arm64)
   │     macos-signing-keychain  ← secret MACOS_SIGNING_BUNDLE
   │     make desktop-embed      → signs + notarizes the embedded wails wrapper
   │     make release-darwin-*   → signs the CLI binaries (hardened runtime)
   │     scripts/build-macos-app → notarizes the CLI zips, builds the .app
   │                               bundles and the .pkg installers, signs them,
   │                               notarizes and staples them
   │     stapler validate        → the job fails if anything is unstapled
   │
   └── job release (ubuntu-latest)
         one GitHub release with every artifact and the changes since the
         previous tag
```

This mirrors the manual `xc release` + `xc release-osx` tasks exactly; those
remain valid for a local dry run on `mac-mini-de-digio`.

## Artifacts

| File | Contents |
| --- | --- |
| `pando-linux-x64.zip`, `pando-linux-arm64.zip` | CLI, UPX-compressed |
| `pando-windows-x64.zip` | CLI, unsigned (no Windows code-signing certificate yet) |
| `pando-darwin-arm64.zip`, `pando-darwin-x64.zip` | CLI, signed + notarized (submit-only) |
| `Pando-arm64.app.zip`, `Pando-x64.app.zip` | `.app` bundle, signed + notarized + stapled |
| `pando-<version>-darwin-<arch>.pkg` | installer, signed + notarized + stapled |

A bare Mach-O cannot carry a stapled ticket, so the loose CLI zips pass only
Gatekeeper's **online** check. The `.pkg` is the artifact to hand to someone who
may install offline.

## The macOS secret

Everything the signing needs is one repository secret, `MACOS_SIGNING_BUNDLE`:
a base64 `.tar.gz` of `~/DIGIO_Software_Signing_Keys`, i.e. both Developer ID
`.p12` files plus the `kvagerc` env file holding their passwords and the notary
credentials (`NOTARY_APPLE_ID`, `NOTARY_TEAM_ID`, `NOTARY_APP_PASSWORD`).

Regenerate or rotate it with:

```bash
# from a machine that can reach the Mac holding the keys
ssh mac-mini-de-digio 'tar czf - -C ~ DIGIO_Software_Signing_Keys | base64' \
  | gh secret set MACOS_SIGNING_BUNDLE --repo digiogithub/pando
```

The value never touches the shell history or a file on disk. On the runner the
`macos-signing-keychain` action unpacks it into `RUNNER_TEMP`, masks every
password it reads, imports the identities into an **ephemeral** keychain, stores
the `pando-notary` profile in it, and deletes the raw `.p12` files. The keychain
itself is deleted by `macos-keychain-cleanup`, which runs with `if: always()`.

To use the same certificates from another repository, set the same secret there:

```bash
gh secret set MACOS_SIGNING_BUNDLE --repo <owner>/<other-repo> < bundle.b64
```

The signing certificates expire (Developer ID certificates last 5 years); when
they are renewed, rebuild the bundle and re-upload the secret.

## Running it

```bash
git tag v0.701.0
git push origin v0.701.0
```

A tag-triggered workflow runs the version of the file **contained in the tagged
commit**, so `.github/workflows/release.yml` must be committed and pushed before
the tag is created.

`workflow_dispatch` re-runs the whole pipeline for an existing tag (input `tag`),
optionally as a draft release — useful to re-cut a release after a failed
notarization without moving the tag.

## Notes and caveats

- Notarization submits half a dozen artifacts to Apple with `--wait`; the macOS
  job typically takes 25–45 minutes and has a 120-minute timeout.
- The GitHub macOS runner is arm64. `pando-darwin-x64` is cross-compiled with
  `clang -arch x86_64`, exactly like the local build; `zig` is installed anyway
  because the Makefile requires it to be present for the non-native arch.
- The desktop wrapper embedded in the Linux and Windows binaries is the one
  built on the Linux runner, matching what the local `xc build` produces.
- Windows binaries are unsigned. Adding an Authenticode signature later is a
  matter of a new step in the same job — the artifacts and the release job do
  not change.

## Reusing this setup elsewhere

For a plain Go project, the entire pipeline is one job:

```yaml
jobs:
  release:
    uses: digiogithub/ci-actions/.github/workflows/release-go.yml@v1
    permissions:
      contents: write
    with:
      binary-name: mytool
      macos-sign: true
      targets: >-
        [{"goos":"linux","goarch":"amd64","suffix":"linux-x64","upx":true},
         {"goos":"darwin","goarch":"arm64","suffix":"darwin-arm64"}]
    secrets:
      MACOS_SIGNING_BUNDLE: ${{ secrets.MACOS_SIGNING_BUNDLE }}
```

Pando does not use that reusable workflow because its build is bespoke (bun,
wails, `.pkg` installers); it calls the individual composite actions instead.
The `ci-actions` README documents the Rust equivalent.
