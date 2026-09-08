#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
compose=(docker compose -p "kaneo-acceptance-$$" -f integration/compose.yml)
log_dir=${KANEO_TEST_LOG_DIR:-$(mktemp -d)}
mkdir -p "$log_dir"

cleanup() {
  status=$?
  trap - EXIT
  "${compose[@]}" logs --no-color > "$log_dir/kaneo.log" 2>&1 || true
  if (( status != 0 )); then
    tail -n 200 "$log_dir/kaneo.log"
  fi
  "${compose[@]}" down --volumes --remove-orphans || status=1
  echo "Kaneo logs: $log_dir/kaneo.log"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"${compose[@]}" up --detach --wait --wait-timeout 240
export KANEO_TEST_ENDPOINT="http://localhost:${KANEO_TEST_PORT:-5173}/api"
TF_ACC=1 go test -count=1 -v -timeout 15m ./internal/provider -run '^TestAcc' "$@"
