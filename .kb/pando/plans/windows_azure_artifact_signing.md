---
created_at: 2026-09-03T09:00:37.84903142Z
updated_at: 2026-09-03T09:10:58.191968169Z
tags:
    - plan
    - ci
    - github-actions
    - release
    - windows
    - signing
    - azure
    - artifact-signing
    - trusted-signing
    - wails
    - pando
---
# Plan: Windows code signing with Azure Artifact Signing (ex Trusted Signing)

## Date
2026-09-03

## Motivation
The release pipeline ([[tag_triggered_release_pipeline]]) signs and notarizes every
macOS artifact but ships **unsigned Windows binaries**. Windows SmartScreen flags
them. Accepted procedure: Azure Artifact Signing (the service formerly named
Azure Trusted Signing; the Azure resource provider is still `Microsoft.CodeSigning`).

## Pre-existing bug found while analysing this
`internal/desktop/embed_binary.go` declares `//go:embed bin/pando-desktop` for
**every non-darwin** GOOS. In `.github/workflows/release.yml` the `linux-windows`
job runs `make desktop-embed` on `ubuntu-latest`, which builds the **Linux** wails
wrapper and leaves it at `internal/desktop/bin/pando-desktop`. The subsequent
`make release-windows-amd64` (zig cross-compile) therefore bakes a **Linux ELF**
into `pando-windows-x64.exe`, so `pando desktop` on Windows is already broken
today, independently of signing. Fixing it requires a native Windows build job —
the same job the signing needs.

## Azure account state (user's subscription, 2026-09-03)
- Subscription: `5a0ec42a-5724-4790-ba2b-519c808820e4`, display name
  `Digio (EV Artifacts signing)` (this is the **subscription** name, NOT the
  signing account name — an Artifact Signing account name is 3–24 alphanumeric
  characters and cannot contain spaces or parentheses).
- Resource group: `digio-artifact-signing`
- **Artifact Signing account: `digio-art-sign-acc`**, location `westeurope`,
  provisioning state `Succeeded`
- **Signing endpoint: `https://weu.codesigning.azure.net/`**
- Identity validation (Organization, Public): `60375e08-5d39-49e0-9541-92dc204ac63c`
- Status: **pending / In Progress** (Microsoft takes 1–20 business days)
- Certificate profile: NOT created yet — blocked on the validation completing.

Useful lookup:

```bash
az account set -s 5a0ec42a-5724-4790-ba2b-519c808820e4
az artifact-signing list -g digio-artifact-signing -o table
```

Once the validation reports `Completed`:

```bash
az artifact-signing certificate-profile create \
  -g digio-artifact-signing --account-name digio-art-sign-acc \
  -n pando-public-trust --profile-type PublicTrust \
  --identity-validation-id 60375e08-5d39-49e0-9541-92dc204ac63c
```

A `--profile-type PublicTrustTest` profile can be created to exercise the pipeline
before the public-trust one is usable.

## Eligibility / cost notes
- Public Trust certificates are available to organizations in the US, Canada, the
  EU, UK, Australia, New Zealand, Japan, South Korea, Singapore, Switzerland,
  Norway and Israel. DIGIO is in Spain, so the EU case applies. The legal entity
  needs a verifiable history of at least three years and up-to-date public records.
- The subscription must be Pay-As-You-Go or Enterprise Agreement; a free trial
  cannot create Artifact Signing resources.
- Basic SKU ≈ 9.99 USD/month, 5,000 signatures/month.
- Region determines the signing endpoint. West Europe → `https://weu.codesigning.azure.net`.

## GitHub authentication (planned, OIDC — no client secret)
App registration + service principal, federated credential against
`https://token.actions.githubusercontent.com`. The `subject` cannot use wildcards
for tags, so bind it to a GitHub **Environment** instead:
`repo:digiogithub/pando:environment:release`.

Role assignment on the signing account:

```bash
APP_ID=$(az ad app list --display-name pando-github-signing --query "[0].appId" -o tsv)
SP_ID=$(az ad sp show --id "$APP_ID" --query id -o tsv)
ACC=$(az artifact-signing show -g digio-artifact-signing -n digio-art-sign-acc --query id -o tsv)
az role assignment create --assignee-object-id "$SP_ID" \
  --assignee-principal-type ServicePrincipal \
  --role "Trusted Signing Certificate Profile Signer" --scope "$ACC"
```

The role may already be listed as `Artifact Signing Certificate Profile Signer`;
check with `az role definition list --scope "$ACC" --query "[].roleName"`.

Repository secrets/vars to create:

| Name | Value |
| --- | --- |
| `AZURE_TENANT_ID` | Entra tenant id |
| `AZURE_CLIENT_ID` | app registration client id |
| `AZURE_SUBSCRIPTION_ID` | `5a0ec42a-5724-4790-ba2b-519c808820e4` |
| `AZURE_SIGNING_ENDPOINT` | `https://weu.codesigning.azure.net` |
| `AZURE_SIGNING_ACCOUNT` | `digio-art-sign-acc` |
| `AZURE_SIGNING_PROFILE` | `pando-public-trust` |

## Pipeline design (agreed, NOT implemented yet)
`Azure/trusted-signing-action` only runs on Windows runners (`windows-2022` /
`windows-2025`, x64 only). There is no usable Linux path: `trusted-signing-cli`
still shells out to `signtool` from the Windows SDK. So the Windows build moves
to a native runner, mirroring what the macOS job does.

1. `.github/workflows/release.yml`
   - `linux-windows` job becomes `linux` only (drop `release-windows-amd64`).
   - New `windows` job on `windows-2022`, `environment: release`,
     `permissions: id-token: write`:
     1. `wails build` natively → `desktop/build/bin/pando-desktop.exe`
     2. `azure/login@v2` (OIDC) + `Azure/trusted-signing-action@v0` over that
        folder, `files-folder-filter: exe` — signs the wrapper **before** embedding,
        exactly like `make desktop-embed` signs+notarizes `Pando.app` on macOS.
     3. copy it to `internal/desktop/bin/pando-desktop` (this also fixes the ELF bug)
     4. `go build` the CLI → `upx` → **then** sign again. The order is mandatory:
        UPX rewrites the PE and would destroy an earlier Authenticode signature.
     5. package with `Compress-Archive` (no `zip` binary on the Windows runner)
        and upload the artifact.
2. `Makefile`: add a `WINDOWS_CODESIGN_WRAPPER` hook inside `build_release`,
   symmetric to the existing `MACOS_CODESIGN_WRAPPER`, so
   `make release-windows-amd64` signs when the wrapper is present.
3. `docs/release-pipeline.md`: document the secrets and the Windows flow.

## Next step
Wait for identity validation `60375e08-5d39-49e0-9541-92dc204ac63c` to reach
`Completed`, create the certificate profile, then implement the pipeline changes.

## Related
[[tag_triggered_release_pipeline]] [[macos_notarize_app_and_desktop_wrapper]]
[[macos_notarize_cli_release_zips]] [[macos_temporary_signing_keychain]]