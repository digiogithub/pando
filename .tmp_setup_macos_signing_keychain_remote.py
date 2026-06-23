from pathlib import Path

p = Path("/Users/digio/www/MCP/Pando/pando/scripts/setup-macos-signing-keychain")
text = p.read_text()
text = text.replace(
'''KEYCHAIN_BASE_NAME="$(normalize_name "$KEYCHAIN_NAME")"
KEYCHAIN_BASE_WITH_EXT="${KEYCHAIN_BASE_NAME}.keychain-db"
KEYCHAIN_PATH="${MACOS_SIGN_KEYCHAIN_PATH:-$HOME/Library/Keychains/$KEYCHAIN_BASE_WITH_EXT}"
KEYCHAIN_PATH_NO_EXT="${KEYCHAIN_PATH%.keychain-db}"
KEYCHAIN_PATH_LEGACY="${KEYCHAIN_PATH_NO_EXT}.keychain"
''',
'''KEYCHAIN_BASE_NAME="$(normalize_name "$KEYCHAIN_NAME")"
KEYCHAIN_PATH="${MACOS_SIGN_KEYCHAIN_PATH:-$HOME/Library/Keychains/${KEYCHAIN_BASE_NAME}-db}"
KEYCHAIN_PATH_NO_EXT="${KEYCHAIN_PATH%-db}"
KEYCHAIN_PATH_LEGACY="${KEYCHAIN_PATH_NO_EXT}.keychain"
'''
)
p.write_text(text)
