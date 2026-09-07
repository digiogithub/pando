---
created_at: 2026-09-07T15:04:30.3603857Z
updated_at: 2026-09-07T15:13:05.191428635Z
tags:
    - feature
    - ci
    - release
    - signing
    - windows
    - azure
---
# Windows Authenticode signing with Azure Trusted Signing (2026-09-07)

Extends the release pipeline described in [[release-pipeline]] /
[[feature_release_pipeline_ci_actions]]: the Windows CLI binary, previously
shipped unsigned, is now Authenticode-signed with Azure Trusted Signing.

## What changed

- `.github/workflows/release.yml`
  - `linux-windows` job now uploads two artifacts instead of one:
    `dist-linux` (the two Linux zips) and `unsigned-windows` (the Windows zip).
    The unsigned artifact deliberately does **not** match `dist-*`.
  - New job `windows-sign` (`runs-on: windows-latest`, `environment: release`,
    `permissions: id-token: write`): downloads `unsigned-windows`, expands the
    zip, `azure/login@v2` via OIDC, `azure/trusted-signing-action@v0`
    (`file-digest: SHA256`, `timestamp-rfc3161: http://timestamp.acs.microsoft.com`),
    verifies with `Get-AuthenticodeSignature` (fails the job unless `Valid`),
    repacks the zip and uploads it as `dist-windows`.
  - `release` job: `needs: [linux-windows, windows-sign, macos]` and
    `download-artifact` gained `pattern: dist-*`, so the unsigned zip can never
    reach a published release.
- `docs/release-pipeline.md`: new "Windows signing (Azure Trusted Signing)"
  section with the resource table, the variable table and the `az`/`gh`
  commands to recreate the whole setup; artifact table and caveats updated.

## Azure resources (already provisioned)

| Piece | Value |
| --- | --- |
| Subscription | `Digio (EV Artifacts signing)` `5a0ec42a-5724-4790-ba2b-519c808820e4` |
| Tenant | `30e30347-734c-4e52-85c3-a3c0a0d3389d` (`digioms.onmicrosoft.com`) |
| Resource group | `digio-artifact-signing` |
| Trusted Signing account | `digio-art-sign-acc` (westeurope, Basic) |
| Account URI | `https://weu.codesigning.azure.net/` |
| Certificate profile | `digio` (PublicTrust, `CN=Digio Soluciones Digitales SL`) |
| Entra app | `pando-github-trusted-signing`, appId `3d9ac0eb-08e2-40b4-b95b-d9c3303d04bf` |
| Service principal objectId | `31ee385a-f0ae-4c49-b77f-b65f6e0d41eb` |
| Federated credential | `pando-release-environment`, subject `repo:digiogithub/pando:environment:release` |

Created in this session with `az ad app create`, `az ad sp create` and
`az ad app federated-credential create`.

## GitHub configuration (already applied)

Environment `release` created via
`gh api -X PUT repos/digiogithub/pando/environments/release`.

Repository **variables** (not secrets — the identity is the OIDC token):
`AZURE_SIGNING_ENDPOINT`, `AZURE_SIGNING_ACCOUNT`, `AZURE_SIGNING_CERT_PROFILE`,
`AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_SUBSCRIPTION_ID`.

## RBAC role name (gotcha)

The role is **`Artifact Signing Certificate Profile Signer`**, id
`2837e146-70d7-4cfd-ad55-7efa6464f958`. Microsoft renamed the service from
Trusted Signing to Artifact Signing and renamed the built-in roles with it, so
every doc and blog post naming `Trusted Signing Certificate Profile Signer`
fails with `Role '…' doesn't exist.` The companion role is
`Artifact Signing Identity Verifier`. List them with:

```bash
az role definition list --scope "<signing account resource id>" \
  --query "[?contains(roleName,'Artifact Signing')].{name:roleName,id:name}" -o tsv
```

Outstanding manual step (the local permission classifier blocks RBAC writes, so
the user runs it):

```bash
az role assignment create \
  --assignee-object-id 31ee385a-f0ae-4c49-b77f-b65f6e0d41eb \
  --assignee-principal-type ServicePrincipal \
  --role "Artifact Signing Certificate Profile Signer" \
  --scope "/subscriptions/5a0ec42a-5724-4790-ba2b-519c808820e4/resourceGroups/digio-artifact-signing/providers/Microsoft.CodeSigning/codeSigningAccounts/digio-art-sign-acc"
```

Without it the signing step fails with an authorization error.

## Design notes

- Signing must happen on a Windows runner: `signtool` plus the Trusted Signing
  dlib have no Linux build, so the Linux-cross-compiled `.exe` travels through
  an artifact to a second job.
- UPX runs before signing (`build_release` in the Makefile). UPX rewrites the PE
  and would strip a signature; appending the certificate table afterwards keeps
  the compressed binary valid.
- OIDC + environment subject was chosen over a client secret: federated
  credential subjects do not support wildcards, and
  `repo:<owner>/<repo>:environment:release` matches every tag build without
  storing a rotating secret.
- Trusted Signing leaf certificates last three days and rotate automatically;
  only the identity validation behind the profile can expire.

## Verification

- `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))"`
  parses and lists jobs `linux-windows, windows-sign, macos, release`.
- Azure resource values read back from `az rest` against the ARM control plane
  (account URI, profile status `Active`, certificate status `Active`).
- `gh variable list -R digiogithub/pando` shows the six variables.
- Role name confirmed with `az role definition list` at the account scope.
- End-to-end signing is unverified until the role assignment above is applied
  and a `v*` tag (or `workflow_dispatch`) runs the pipeline.
