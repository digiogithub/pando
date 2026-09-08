---
created_at: 2026-09-07T22:07:29.851335757Z
updated_at: 2026-09-08T20:16:24.65315277Z
tags:
    - fix
    - ci
    - release
    - signing
    - git-in-track
---
# git-in-track release pipeline: port of the Pando signed-release setup (2026-09-08)

The `digiogithub/git-in-track` repository copied Pando's signed-release workflow
(`.github/workflows/release.yml`, shared composite actions `digiogithub/ci-actions@v1`)
but tag `v1.0.0` failed. Related: [[feature_release_pipeline_ci_actions]].

## What was wrong

Three independent problems, all configuration rather than workflow code:

1. **Windows job** — `##[error]Login failed with Error: Using auth-type: SERVICE_PRINCIPAL.
   Not all values are present. Ensure 'client-id' and 'tenant-id' are supplied.`
   The repository had **zero** Actions variables; `vars.AZURE_*` all resolved empty.
2. **macOS job** — the keychain action logged `SIGNING_BUNDLE:` (empty) and
   `Unpacked 0 file(s) into the signing keys directory.` The repository had **zero**
   Actions secrets; `secrets.MACOS_SIGNING_BUNDLE` was missing.
3. **CI job "Workflows (YAML + actionlint)"** — actionlint failed with three
   `SC2129` shellcheck findings on the `Resolve the version` steps
   (`release.yml:141`, `:213`, `:321`): four individual `echo ... >> "$GITHUB_OUTPUT"`
   redirects instead of one grouped redirect.

## What was changed

- Copied the six Azure variables from `digiogithub/pando` to
  `digiogithub/git-in-track` with `gh variable set` (repository level, matching
  Pando): `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID`,
  `AZURE_SIGNING_ENDPOINT` (`https://weu.codesigning.azure.net/`),
  `AZURE_SIGNING_ACCOUNT` (`digio-art-sign-acc`),
  `AZURE_SIGNING_CERT_PROFILE` (`digio`).
- Rewrote the three `Resolve the version` steps in
  `/www/git-in-track/.github/workflows/release.yml` to use a single
  `{ echo ...; } >> "$GITHUB_OUTPUT"` block. Verified with a local `actionlint`
  run over `.github/workflows` (exit 0). **Still uncommitted in the working tree.**
- Added a second federated credential to the Entra app
  `3d9ac0eb-08e2-40b4-b95b-d9c3303d04bf` (object `23074770-…`), name
  `gintrack-release-environment`, subject
  `repo:digiogithub/git-in-track:environment:release`, issuer
  `https://token.actions.githubusercontent.com`, audience
  `api://AzureADTokenExchange`. The Trusted Signing account, certificate profile
  and role assignment are shared with Pando, so nothing else was needed on Azure.
- Set `MACOS_SIGNING_BUNDLE` in git-in-track. **A repository secret cannot be read
  back from GitHub** (write-only), so it cannot be copied from Pando; it has to be
  regenerated from the machine holding the keys:

  ```bash
  ssh mac-mini-de-digio 'tar czf - -C ~ DIGIO_Software_Signing_Keys | base64' \
    | gh secret set MACOS_SIGNING_BUNDLE --repo digiogithub/git-in-track
  ```

  `~/DIGIO_Software_Signing_Keys` on `mac-mini-de-digio` (user `digio`) holds
  `certificado.p12`, `clave.p12`, `developerID_application.{cer,p12}`,
  `developerID_installer.cer` and `kvagerc`; the base64 bundle is ~27 KB, well
  under the 48 KB GitHub secret limit. The value never touches disk or the shell
  history on the way through.

## Verification

- `gh variable list -R digiogithub/git-in-track` shows the six variables;
  `gh secret list` shows `MACOS_SIGNING_BUNDLE`.
- `az ad app federated-credential list` now returns both subjects (pando and
  git-in-track).
- `actionlint` clean over the whole `.github/workflows` directory.
- The `release` GitHub environment already existed in both repos with identical
  (empty) protection rules, so no environment change was required.
- Pending: commit/push the lint fix and re-run the release
  (`gh workflow run Release -R digiogithub/git-in-track -f tag=v1.0.0`).
