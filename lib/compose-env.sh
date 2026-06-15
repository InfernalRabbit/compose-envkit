#!/bin/sh
# ============================================
# compose-env.sh — universal docker compose env-chain wrapper.
#
# Part of compose-envkit. Project-agnostic, POSIX sh (no bashisms).
#
# Builds COMPOSE_ENV_FILES from two layers and then dispatches to docker /
# docker compose. This is the engine behind the ./docker shim.
#
#   Layer 1 — project chain: `<project>/.docker-env-chain` (or built-in
#     defaults). These are the files compose uses for ${VAR} interpolation in
#     the YAML itself. Last file wins.
#
#   Layer 2 — container env_file:: auto-discovered from docker-compose*.yml via
#     parse-compose-env-files.sh. Those files are ALREADY declared in the YAML
#     as a service's env_file:; we additionally fold them into the interpolation
#     context so per-env overrides defined inside them apply to compose-time
#     refs such as `ports: "${APP_PORT:-3000}:3000"`. Native compose does NOT do
#     this — closing that gap is the whole reason this kit exists.
#
# .docker-env-chain entries support ${ENV} / ${COMPOSE_ENV} and ${HOST} /
# ${HOSTNAME} (machine hostname) substitution. Non-existent files are silently
# skipped — so an optional per-machine `.${HOSTNAME}.env` just no-ops elsewhere.
#
# Usage (normally invoked via a shim — see bin/docker):
#   compose-env.sh <PROJECT_DIR> compose <args>   — passthrough to docker compose
#   compose-env.sh <PROJECT_DIR> env-files        — print chain (one path/line)
#   compose-env.sh <PROJECT_DIR> <other>          — passthrough to docker
#
# COMPOSE_ENV resolution: shell env > <PROJECT_DIR>/.env > "dev".
# COMPOSE_DEPTH (env var) controls the YAML search depth (default 3).
# ============================================

set -eu

PROJECT_DIR="${1:?compose-env.sh: PROJECT_DIR required}"
shift

[ -d "$PROJECT_DIR" ] || { echo "compose-env.sh: PROJECT_DIR '$PROJECT_DIR' is not a directory" >&2; exit 2; }

# Resolve COMPOSE_ENV from .env (strip the key, a trailing CR from CRLF files,
# and surrounding whitespace — `cut -d=` would also truncate values with '=').
_FILE_ENV=$(grep -m1 '^COMPOSE_ENV=' "${PROJECT_DIR}/.env" 2>/dev/null \
  | sed -e 's/^COMPOSE_ENV=//' -e 's/\r$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' || true)
ENV=${COMPOSE_ENV:-${_FILE_ENV:-dev}}

# Machine hostname for per-machine chain overrides (.${HOST}.env / .${HOSTNAME}.env).
# An exported HOSTNAME wins (lets CI / tests pin it); otherwise the hostname
# command, with fallbacks so the substitution never yields an empty token.
# Sanitize to [A-Za-z0-9._-]: the value is spliced into a sed program below, so a
# hostname containing the sed delimiter (|) or & must never reach it.
_HOST=${HOSTNAME:-$(hostname 2>/dev/null || uname -n 2>/dev/null || echo unknown)}
_HOST=$(printf '%s' "$_HOST" | tr -cd 'A-Za-z0-9._-')
[ -n "$_HOST" ] || _HOST=unknown

# Depth for the docker-compose*.yml search (Layer 2). Override with COMPOSE_DEPTH.
_DEPTH=${COMPOSE_DEPTH:-3}

# --- Build chain ---

_FILES=""
_append_if_exists() {
  [ -f "$1" ] && _FILES="${_FILES:+${_FILES},}${1}"
  return 0
}

# Layer 1: project chain (.docker-env-chain or built-in defaults)
if [ -f "${PROJECT_DIR}/.docker-env-chain" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      ''|\#*) continue ;;
    esac
    line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -z "$line" ] && continue
    line=$(printf '%s' "$line" | sed "s|\${ENV}|${ENV}|g; s|\${COMPOSE_ENV}|${ENV}|g; s|\${HOST}|${_HOST}|g; s|\${HOSTNAME}|${_HOST}|g")
    _append_if_exists "${PROJECT_DIR}/${line}"
  done < "${PROJECT_DIR}/.docker-env-chain"
else
  # Built-in defaults — simple single-project layout.
  for f in "${PROJECT_DIR}/.env" "${PROJECT_DIR}/.${ENV}.env" "${PROJECT_DIR}/.secrets.env"; do
    _append_if_exists "$f"
  done
fi

# Layer 2: container env_file: paths auto-discovered from compose YAML.
# These files are ALREADY declared in docker-compose*.yml as env_file: — we add
# them to the interpolation context for compose-time ${VAR} overrides (e.g.
# `ports: "${APP_PORT:-3000}:3000"` where APP_PORT lives in .app.dev.env).
# Uses the shared awk parser (single source of truth) when available.
_PARSER="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/parse-compose-env-files.sh"
if [ -f "$_PARSER" ]; then
  _COMPOSE_YAMLS=$(find "$PROJECT_DIR" -maxdepth "$_DEPTH" -name 'docker-compose*.yml' -type f 2>/dev/null | tr '\n' ' ')
  if [ -n "$_COMPOSE_YAMLS" ]; then
    # shellcheck disable=SC2086
    _PARSED=$(sh "$_PARSER" "$ENV" $_COMPOSE_YAMLS 2>/dev/null || true)
    if [ -n "$_PARSED" ]; then
      _oldIFS=$IFS
      IFS='
'
      for _rel in $_PARSED; do
        [ -z "$_rel" ] && continue
        # Path may be absolute (if the parser got an absolute compose YAML) or
        # relative (to PROJECT_DIR). Normalize to absolute.
        case "$_rel" in
          /*) _abs="$_rel" ;;
          *)  _abs="${PROJECT_DIR}/${_rel}" ;;
        esac
        [ -f "$_abs" ] || continue
        case ",${_FILES}," in
          *",${_abs},"*) ;;   # dedupe — already in chain
          *) _FILES="${_FILES:+${_FILES},}${_abs}" ;;
        esac
      done
      IFS=$_oldIFS
    fi
  fi
fi

export COMPOSE_ENV_FILES="$_FILES"

# --- Dispatch ---

case "${1:-}" in
  env-files)
    [ -n "$COMPOSE_ENV_FILES" ] && printf '%s\n' "$COMPOSE_ENV_FILES" | tr ',' '\n'
    exit 0
    ;;
  compose)
    shift
    cd "$PROJECT_DIR"
    exec docker compose "$@"
    ;;
  *)
    exec docker "$@"
    ;;
esac
