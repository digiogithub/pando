---
created_at: 2026-07-15T22:02:50.408606201Z
updated_at: 2026-07-15T22:02:50.408606201Z
tags:
    - fix
    - macos
    - build
    - pkg
    - installer
    - relocation
    - pando
---
# Fix: .pkg installs nothing to /Applications (bundle relocation)

## Date
2026-07-16

## Symptom
After installing `pando-<ver>-darwin-arm64.pkg`, the launcher works but the app
is missing:

```
$ pando --version
/usr/local/bin/pando: line 2: /Applications/Pando.app/Contents/MacOS/pando-bin: No such file or directory
```

`/Applications/Pando.app` does not exist, yet `pkgutil --pkgs` lists both
`es.digio.pando` and `es.digio.pando.launcher` as installed.

## Root cause
macOS Installer **bundle relocation**. `/var/log/install.log` showed:

```
PackageKit: Applications/Pando.app relocated to Users/digio/.../dist/Pando-arm64.app
```

By default a pkg component carrying a bundle is relocatable: Installer looks up
the component's bundle identifier (`es.digio.pando`) via LaunchServices and, if it
finds an existing copy of that bundle ANYWHERE (here the `dist/` build output, or
any old Pando.app), it redirects the payload onto that existing path instead of
the declared `/Applications`. The receipt is still written, so it looks
"installed" while `/Applications/Pando.app` stays absent. On a machine with a
pre-existing Pando.app anywhere, the install is hijacked.

## Fix
`scripts/build-macos-app` — build the app component with a component-plist that
sets `BundleIsRelocatable=false`, pinning the install to `/Applications`:

```bash
pkgbuild --analyze --root "$app_stage" "$comp_plist"
idx=0
while /usr/libexec/PlistBuddy -c "Print :${idx}:BundleIsRelocatable" "$comp_plist" >/dev/null 2>&1; do
    /usr/libexec/PlistBuddy -c "Set :${idx}:BundleIsRelocatable false" "$comp_plist" >/dev/null 2>&1 || true
    idx=$((idx + 1))
done
pkgbuild --root "$app_stage" --component-plist "$comp_plist" \
    --identifier es.digio.pando --version "$BUNDLE_VERSION" --install-location "/" "$app_pkg"
```

## Verification
- `bash -n scripts/build-macos-app` → OK.
- On mac: `pkgbuild --analyze` + the PlistBuddy loop flips
  `BundleIsRelocatable` true → false (1 bundle processed).

## Interim unblock (no rebuild)
The current pkg still relocates. To install into /Applications now: remove the
build-output apps that trigger relocation and forget the stale receipt, then
reinstall:
```
sudo rm -rf .../dist/Pando-arm64.app .../dist/Pando-x64.app
sudo pkgutil --forget es.digio.pando
```
Proper fix is to rebuild with the patched script (needs commit + push + pull on
the mac).

## Related
[[macos_notarize_app_and_desktop_wrapper]] [[macos_zip_verify_sigpipe_false_negative]]
[[macos_desktop_signing_fix]]
