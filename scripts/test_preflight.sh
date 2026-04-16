#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
DEV_COMPOSE_FILE="${ROOT_DIR}/docker-compose.dev.yml"
TEST_COMPOSE_FILE="${ROOT_DIR}/docker-compose.test.yml"

fail() {
  printf 'test preflight failed: %s\n' "$1" >&2
  exit 1
}

if ! command -v docker >/dev/null 2>&1; then
  fail "docker is required. SuperPlane's supported local test workflow runs through docker compose, not host-native DB/Python setup."
fi

if ! docker compose version >/dev/null 2>&1; then
  fail "docker compose is required for local tests."
fi

if ! docker info >/dev/null 2>&1; then
  fail "the Docker daemon is not reachable. Start Docker, then rerun 'make test.setup' or 'make test.local.full'."
fi

if [ ! -f "${DEV_COMPOSE_FILE}" ]; then
  fail "docker-compose.dev.yml is missing."
fi

if [ ! -f "${TEST_COMPOSE_FILE}" ]; then
  fail "docker-compose.test.yml is missing."
fi

if [ ! -f "${ROOT_DIR}/agent/.env" ]; then
  fail "agent/.env is missing. Run a setup target such as 'make test.setup' so the canonical containerized workflow can create it."
fi

if ! docker compose -f "${DEV_COMPOSE_FILE}" -f "${TEST_COMPOSE_FILE}" config >/dev/null 2>&1; then
  fail "docker compose test config is invalid. Check docker-compose.dev.yml, docker-compose.test.yml, and agent/.env."
fi

printf 'test preflight ok: docker compose workflow is available.\n'
