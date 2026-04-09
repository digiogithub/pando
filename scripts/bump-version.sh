#!/usr/bin/env bash
set -euo pipefail

NEW_VERSION="${1:?Usage: $0 <version> (e.g. 0.2.0)}"

echo "Bumping version to $NEW_VERSION..."

# Update internal/version/version.go
VERSION_FILE="internal/version/version.go"
if [ -f "$VERSION_FILE" ]; then
  sed -i.bak "s/Version = \"[^\"]*\"/Version = \"$NEW_VERSION\"/" "$VERSION_FILE"
  rm -f "${VERSION_FILE}.bak"
  echo "  Updated $VERSION_FILE"
fi

# Update desktop/wails.json productVersion
WAILS_JSON="desktop/wails.json"
if [ -f "$WAILS_JSON" ]; then
  # Use python for reliable JSON editing if available, else sed
  if command -v python3 &>/dev/null; then
    python3 -c "
import json, sys
with open('$WAILS_JSON') as f: data = json.load(f)
data.setdefault('info', {})['productVersion'] = '$NEW_VERSION'
with open('$WAILS_JSON', 'w') as f: json.dump(data, f, indent=2)
print('  Updated $WAILS_JSON')
"
  else
    sed -i.bak "s/\"productVersion\": \"[^\"]*\"/\"productVersion\": \"$NEW_VERSION\"/" "$WAILS_JSON"
    rm -f "${WAILS_JSON}.bak"
    echo "  Updated $WAILS_JSON"
  fi
fi

echo "Done. Version is now $NEW_VERSION"
echo "Remember to: git tag desktop/v$NEW_VERSION && git push --tags"
