---
created_at: 2026-10-08T08:06:00.084237572Z
updated_at: 2026-10-08T08:06:00.084237572Z
tags:
    - change
    - ci
    - release
    - signing
    - azure
    - macos
    - bitacora
---
# Bitácora: code-signing secrets/variables setup (2026-10-08)

Requested by the Pips session for José: give `digiogithub/bitacora` (local `/www/Bitacora/bitacora`) the same signing configuration as Pando and git-in-track. Same procedure as [[gintrack_release_pipeline_port]]; Azure details in [[windows-authenticode-azure-trusted-signing]]. Workflows of Bitácora were NOT touched (owned by the Bitácora instance).

## Azure (no new resources, no cost)
- Reused: subscription `Digio (EV Artifacts signing)`, RG `digio-artifact-signing`, Trusted Signing account `digio-art-sign-acc` (westeurope), certificate profile `digio`, Entra app `pando-github-trusted-signing` (appId `3d9ac0eb-08e2-40b4-b95b-d9c3303d04bf`), SP `31ee385a-…` which already holds `Artifact Signing Certificate Profile Signer` on the account (verified with `az role assignment list`).
- Added two federated credentials to that app (OIDC, no client secret), following the pattern already used for gwork/docsai/git-in-track:
  - `bitacora-release-environment` subject `repo:digiogithub/bitacora:environment:release`
  - `bitacora-release-environment-immutable` subject `repo:digiogithub@8157515/bitacora@1407402731:environment:release` (GitHub immutable-ID subject format: owner id 8157515, repo id 1407402731)
  - issuer `https://token.actions.githubusercontent.com`, audience `api://AzureADTokenExchange`.

## GitHub (digiogithub/bitacora)
- Environment `release` created (`gh api -X PUT repos/digiogithub/bitacora/environments/release`).
- Repository variables copied from Pando: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID`, `AZURE_SIGNING_ENDPOINT`, `AZURE_SIGNING_ACCOUNT`, `AZURE_SIGNING_CERT_PROFILE`.
- Secret `MACOS_SIGNING_BUNDLE` regenerated from mac-mini-de-digio: `ssh mac-mini-de-digio 'tar czf - -C ~ DIGIO_Software_Signing_Keys | base64' | gh secret set MACOS_SIGNING_BUNDLE --repo digiogithub/bitacora` (value never on disk/shell history). `PANDO_BETTERSTACK_TOKEN` is Pando-specific and not copied.
- No Pando/git-in-track credentials were changed or rotated.

## Notes for the Bitácora release workflow
Reference: `/www/MCP/Pando/pando/.github/workflows/release.yml` (copy at `/www/git-in-track/.github/workflows/release.yml`). Windows job needs `environment: release` + `permissions: id-token: write`, `azure/login@v2` (OIDC with the vars), `azure/trusted-signing-action@v0` (SHA256, timestamp `http://timestamp.acs.microsoft.com`), `Get-AuthenticodeSignature` check. macOS uses `digiogithub/ci-actions@v1` composite actions consuming `MACOS_SIGNING_BUNDLE`. The OIDC subject only matches jobs running in environment `release`.

## Verification
`gh variable list` shows the 6 vars, `gh secret list` shows `MACOS_SIGNING_BUNDLE`, `az ad app federated-credential create` returned both names. End-to-end signing unverified until Bitácora's release workflow runs.
