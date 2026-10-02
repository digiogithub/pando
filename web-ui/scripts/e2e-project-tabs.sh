#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_UI_DIR="$ROOT_DIR/web-ui"
TMP_DIR="$(mktemp -d)"
WORK_DIR="$TMP_DIR/app-workdir"
HOME_DIR="$TMP_DIR/home"
XDG_CONFIG_HOME="$HOME_DIR/.config"
APP_LOG="$TMP_DIR/pando-app.log"
BIN_PATH="$TMP_DIR/pando"
APP_PID=""
CREATED_PROJECT_DIR=""

cleanup() {
  if [[ -n "${APP_PID}" ]] && kill -0 "${APP_PID}" 2>/dev/null; then
    kill "${APP_PID}" 2>/dev/null || true
    for _ in {1..50}; do
      if ! kill -0 "${APP_PID}" 2>/dev/null; then
        break
      fi
      sleep 0.1
    done
    if kill -0 "${APP_PID}" 2>/dev/null; then
      kill -9 "${APP_PID}" 2>/dev/null || true
    fi
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

mkdir -p "${WORK_DIR}" "${XDG_CONFIG_HOME}"
cat > "${WORK_DIR}/.pando.toml" <<'EOF'
[Projects]
Enabled = true
EOF

if [[ -z "${PANDO_E2E_PROJECT_DIR:-}" ]]; then
  CREATED_PROJECT_DIR="${TMP_DIR}/project-tabs-workspace"
  mkdir -p "${CREATED_PROJECT_DIR}"
  printf 'project tabs e2e\n' > "${CREATED_PROJECT_DIR}/README.txt"
  export PANDO_E2E_PROJECT_DIR="${CREATED_PROJECT_DIR}"
fi

if [[ -z "${PLAYWRIGHT_CHROME:-}" ]] && [[ -x /usr/bin/google-chrome ]]; then
  export PLAYWRIGHT_CHROME=/usr/bin/google-chrome
fi

PORT="$(
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
export PANDO_E2E_BASE_URL="https://127.0.0.1:${PORT}"

cd "${WEB_UI_DIR}"
bun run build:embedded

cd "${ROOT_DIR}"
go build -o "${BIN_PATH}" .

(
  cd "${WORK_DIR}"
  HOME="${HOME_DIR}" XDG_CONFIG_HOME="${XDG_CONFIG_HOME}" "${BIN_PATH}" app --host 127.0.0.1 --port "${PORT}" >"${APP_LOG}" 2>&1 &
  echo $! > "${TMP_DIR}/app.pid"
)
APP_PID="$(cat "${TMP_DIR}/app.pid")"

for _ in {1..120}; do
  if curl --silent --show-error --fail --insecure "${PANDO_E2E_BASE_URL}/health" >/dev/null; then
    break
  fi
  sleep 0.5
done

if ! curl --silent --show-error --fail --insecure "${PANDO_E2E_BASE_URL}/health" >/dev/null; then
  cat "${APP_LOG}" >&2 || true
  echo "E2E result: FAIL (app did not become healthy)" >&2
  exit 1
fi

cd "${WEB_UI_DIR}"
bun run test:e2e -- e2e/project-tabs.spec.ts
echo "E2E result: PASS (project-tabs.spec.ts)"
