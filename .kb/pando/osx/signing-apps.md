# How to signing apps into osx

1. Get the id

```
security find-identity -v -p codesigning
```

2. Select the Developer ID Application of the company

```
APP="build/bin/MyApp.app"
IDENTITY="HASH"

codesign --force --deep --options runtime --timestamp \
  --sign "$IDENTITY" "$APP"

```
