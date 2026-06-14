#!/bin/sh
# ============================================
# parse-compose-env-files.sh — portable env_file: parser for compose YAMLs.
#
# Part of compose-envkit. Project-agnostic, POSIX sh + portable awk (no gawk
# extensions). Extracts the `env_file:` paths declared by services in one or
# more docker-compose YAML files, so they can be fed into COMPOSE_ENV_FILES for
# compose-time ${VAR} interpolation (Layer 2 — see lib/compose-env.sh).
#
# Usage:
#   parse-compose-env-files.sh <env-value> [--service <name>] <compose-yaml>...
#
#   <env-value>       value of COMPOSE_ENV used for ${COMPOSE_ENV:-default}
#                     substitution inside env_file: paths.
#   --service <name>  emit env_file: only for the named service (default: all).
#
# Output:
#   One path per line. Paths are relative to the directory of the compose file
#   that declared them (as the compose spec requires), with any leading ./
#   stripped. Final result is de-duplicated, order preserved.
#
# Supported env_file: forms in the YAML:
#   env_file: .app.env                     (single scalar)
#   env_file:                              (short list)
#     - .app.env
#   env_file:                              (long-form list)
#     - path: .app.env
#       required: false
# ============================================

set -eu

ENV_VAL="${1:?env-value required (e.g. dev)}"; shift
SERVICE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --service) SERVICE="${2:-}"; shift 2 ;;
    --service=*) SERVICE="${1#*=}"; shift ;;
    --) shift; break ;;
    *) break ;;
  esac
done

[ "$#" -eq 0 ] && exit 0

awk -v env_val="$ENV_VAL" -v svc_filter="$SERVICE" '
  function indent(line,   n) {
    n = match(line, /[^ ]/)
    return (n == 0) ? -1 : n - 1
  }
  function substitute(s) {
    gsub(/\$\{COMPOSE_ENV:-[^}]*\}/, env_val, s)
    gsub(/\$\{COMPOSE_ENV\}/, env_val, s)
    return s
  }
  function file_dir(f,   d) {
    d = f
    if (sub(/\/[^\/]+$/, "", d) && d != "") return d
    return "."
  }
  function service_matches() {
    return (svc_filter == "" || current == svc_filter)
  }
  function emit(path,   dir, full) {
    if (!service_matches()) return
    dir = file_dir(FILENAME)
    sub(/^\.\//, "", dir)
    full = (dir == "." || dir == "") ? path : dir "/" path
    print full
  }
  FNR == 1 { in_services=0; current=""; in_env_file=0 }
  /^#/ { next }
  { ind = indent($0) }
  /^services:[[:space:]]*$/ { in_services=1; next }
  in_services && ind == 0 && /^[a-z]/ { in_services=0 }
  !in_services { next }

  # Service header (indent=2)
  ind == 2 && /^  [a-zA-Z][a-zA-Z0-9_-]*:[[:space:]]*$/ {
    current = $0
    sub(/^  /, "", current); sub(/:.*$/, "", current)
    in_env_file = 0
    next
  }

  # env_file: <single value>
  ind == 4 && match($0, /^[[:space:]]+env_file:[[:space:]]+[^[:space:]]/) {
    rest = $0
    sub(/^[[:space:]]+env_file:[[:space:]]+/, "", rest)
    emit(substitute(rest))
    next
  }

  # env_file: (list follows)
  ind == 4 && /^[[:space:]]+env_file:[[:space:]]*$/ {
    in_env_file = 1
    next
  }

  # Exit env_file block (sibling property at indent=4)
  in_env_file && ind <= 4 { in_env_file = 0 }

  # - path: <value> (long-form)
  in_env_file && match($0, /^[[:space:]]+-[[:space:]]+path:[[:space:]]+/) {
    rest = $0
    sub(/^[[:space:]]+-[[:space:]]+path:[[:space:]]+/, "", rest)
    emit(substitute(rest))
    next
  }

  # - <value> (short list)
  in_env_file && match($0, /^[[:space:]]+-[[:space:]]+/) {
    rest = $0
    sub(/^[[:space:]]+-[[:space:]]+/, "", rest)
    if (rest ~ /^(path|required):/) next
    emit(substitute(rest))
  }
' "$@" | awk '!seen[$0]++'
