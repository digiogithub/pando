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
   ├── job windows-sign (windows-latest, environment: release)
   │     azure/login            ← OIDC, no secret
   │     trusted-signing-action → Authenticode signature + RFC 3161 timestamp
   │     Get-AuthenticodeSignature → the job fails if the status is not Valid
   │     repacks pando-windows-x64.zip around the signed .exe
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
| `pando-windows-x64.zip` | CLI, UPX-compressed, Authenticode-signed (Azure Trusted Signing) |
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

## Windows signing (Azure Trusted Signing)

The Windows `.exe` is cross-compiled on the Linux runner, then signed on a
Windows runner — `signtool` and the Trusted Signing dlib have no Linux build.
UPX runs **before** signing (it rewrites the PE and would strip the signature);
the Authenticode certificate table is appended afterwards, so the compressed
binary stays signed.

There is no secret: the `windows-sign` job runs in the `release` GitHub
environment and mints an OIDC token that a federated credential on the Entra
application accepts. The Azure side is:

| Piece | Value |
| --- | --- |
| Subscription | `Digio (EV Artifacts signing)` |
| Resource group | `digio-artifact-signing` |
| Trusted Signing account | `digio-art-sign-acc` (West Europe, Basic) |
| Account URI | `https://weu.codesigning.azure.net/` |
| Certificate profile | `digio` (`PublicTrust`, `CN=Digio Soluciones Digitales SL`) |
| Entra app | `pando-github-trusted-signing` |
| Federated credential | subject `repo:digiogithub/pando:environment:release` |
| Role | `Artifact Signing Certificate Profile Signer` on the account |

The workflow reads all of it from **repository variables** (none of these is
sensitive — the identity is the federated token, not a value stored here):

| Variable | Value |
| --- | --- |
| `AZURE_SIGNING_ENDPOINT` | `https://weu.codesigning.azure.net/` |
| `AZURE_SIGNING_ACCOUNT` | `digio-art-sign-acc` |
| `AZURE_SIGNING_CERT_PROFILE` | `digio` |
| `AZURE_TENANT_ID` | the DIGIO tenant id |
| `AZURE_CLIENT_ID` | app id of `pando-github-trusted-signing` |
| `AZURE_SUBSCRIPTION_ID` | subscription holding the signing account |

Recreate the whole Azure side from scratch with:

```bash
APP_ID=$(az ad app create --display-name pando-github-trusted-signing \
  --sign-in-audience AzureADMyOrg --query appId -o tsv)
SP_ID=$(az ad sp create --id "$APP_ID" --query id -o tsv)

az ad app federated-credential create --id "$APP_ID" --parameters '{
  "name": "pando-release-environment",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:digiogithub/pando:environment:release",
  "audiences": ["api://AzureADTokenExchange"]
}'

az role assignment create --assignee-object-id "$SP_ID" \
  --assignee-principal-type ServicePrincipal \
  --role "Artifact Signing Certificate Profile Signer" \
  --scope "$(az resource list \
      --resource-type Microsoft.CodeSigning/codeSigningAccounts \
      --query '[0].id' -o tsv)"
```

Then create the `release` environment and the variables:

```bash
gh api -X PUT repos/digiogithub/pando/environments/release --input /dev/null
gh variable set AZURE_CLIENT_ID -R digiogithub/pando -b "$APP_ID"
# …and the other five variables from the table above
```

The role was called `Trusted Signing Certificate Profile Signer` until the
service was renamed to Artifact Signing; `az role assignment create` rejects the
old name. Its id, `2837e146-70d7-4cfd-ad55-7efa6464f958`, is stable and can be
passed to `--role` instead.

To sign from another repository, repeat only the federated credential (with
that repository's subject) and the variables; the account, the profile and the
role assignment are shared.

Certificates inside a Trusted Signing profile are short-lived (three days) and
rotate automatically — nothing to renew by hand. The **identity validation**
behind the profile does expire; if it lapses, signing fails with an
authorization error and the profile must be revalidated in the portal.

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
- The `linux-windows` job uploads the Windows zip as `unsigned-windows`, i.e.
  deliberately *not* matching the `dist-*` pattern the release job globs, so a
  release can never accidentally ship the unsigned binary.
- A Trusted Signing signature does not remove the Windows SmartScreen prompt on
  day one: reputation still builds per publisher over downloads.

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
