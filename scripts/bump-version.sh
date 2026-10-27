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

echo "Done. Version is now $NEW_VERSION"
echo "Remember to: git tag v$NEW_VERSION && git push --tags"
