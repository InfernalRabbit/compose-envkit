#!/bin/sh
# ============================================
# env-debug.sh — env-chain inspector for docker-compose projects.
#
# Part of compose-envkit. Project-agnostic, POSIX sh + portable awk. Treats the
# current working directory as PROJECT_DIR. Every source of truth is dynamic:
# the shim (./docker env-files), compose (./docker compose config --services),
# and the compose YAML (env_file: directives). No project-specific names baked
# into the code.
#
# Usage:
#   cd <project-dir> && sh <path>/env-debug.sh [mode] [filters]
#
# Modes (mutually exclusive):
#   --chain      (default) Which env files load, and in what order.
#   --diff       Per file: what EACH file adds (+) / overrides (~) / repeats (·)
#                — simulates the "last wins" merge semantics.
#   --effective  Final per-service values (via docker compose config; every
#                ${VAR:-default} already resolved).
#   --files      Bare list of loaded file paths (one per line, machine-readable).
#   --trace      Call stack for one variable: where it is defined in a container
#                env_file:, which ${REF}s it contains, where each REF is set in
#                the project chain, which fallback default fires, and the final
#                effective value. Requires --var <NAME>.
#   --value      Print ONE resolved value of a project-level variable
#                (alias --get). Sources ONLY the project chain (.docker-env-chain:
#                .env -> .${ENV}.env -> .secrets.env) in load order (last-wins)
#                inside a POSIX sh subshell under `set +u`, so ${VAR} /
#                ${VAR:-default} / ${VAR:+...} expand exactly like compose does,
#                picking up overlay + secret layers. Does NOT source container
#                env_file: paths (those hold bare compose-refs, unsafe to
#                shell-source). Plain stdout (no decoration) — for make/scripts.
#                Requires --var <NAME>.
#
# Filters (combine with any mode):
#   --service <name>   Restrict container env_file: files to one service.
#                      Valid names come from docker compose config --services.
#                      Alias: --container.
#   --var <NAME>       Track only this variable: in chain/files show only files
#                      where it occurs; in diff only lines with this key; in
#                      effective only services that define it.
#
# Examples:
#   sh env-debug.sh --diff --var DB_HOST
#   sh env-debug.sh --effective --service web
#   sh env-debug.sh --files
#   sh env-debug.sh --chain --var APP_URL
#   sh env-debug.sh --trace --var DB_HOST          # call stack
#   sh env-debug.sh --value --var COMPOSE_FILE     # one resolved value (for make)
# ============================================

set -eu

# --- Args ---

MODE="chain"
SERVICE=""
VAR_FILTER=""

print_help() {
  awk '
    /^# =====/ { n++; if (n == 2) exit; next }
    n == 1 { sub(/^# ?/, ""); print }
  ' "$0"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --chain)                        MODE="chain" ;;
    --diff)                         MODE="diff" ;;
    --effective|--show-effective)   MODE="effective" ;;
    --files)                        MODE="files" ;;
    --trace|--stack)                MODE="trace" ;;
    --value|--get)                  MODE="value" ;;
    --service|--container)          SERVICE="${2:-}"; shift ;;
    --service=*|--container=*)      SERVICE="${1#*=}" ;;
    --var|-V)                       VAR_FILTER="${2:-}"; shift ;;
    --var=*)                        VAR_FILTER="${1#*=}" ;;
    --help|-h)                      print_help; exit 0 ;;
    *) echo "Unknown arg: $1" >&2
       echo "Run: $0 --help" >&2
       exit 2 ;;
  esac
  shift
done

# Project dir = cwd. The shim (./docker) and compose files must live here.
PROJECT_DIR="$PWD"
if [ ! -x "./docker" ] && [ ! -f "./docker" ]; then
  echo "ERROR: ./docker shim not found in $PROJECT_DIR" >&2
  echo "  Run env-debug.sh from a project directory that has the compose+shim." >&2
  exit 2
fi

# --- Early mode: value (one resolved project-level var, plain stdout) ---
# Short-circuit BEFORE heavy discovery (compose config / per-service parsing):
# value mode needs only the project chain. We source it in a POSIX sh subshell
# in load order (last-wins), then echo the variable — ${VAR}/${VAR:-d}/${VAR:+x}
# expand exactly like compose, picking up overlay + secret layers.
if [ "$MODE" = "value" ]; then
  if [ -z "$VAR_FILTER" ]; then
    echo "ERROR: --value requires --var <NAME>." >&2
    exit 2
  fi

  # ENV for ${ENV}/${COMPOSE_ENV} substitution in .docker-env-chain templates.
  # Same rule as the shim: shell COMPOSE_ENV > .env > "dev".
  _v_file_env=$(grep -m1 '^COMPOSE_ENV=' .env 2>/dev/null | cut -d= -f2- || true)
  _v_env=${COMPOSE_ENV:-${_v_file_env:-dev}}

  # IMPORTANT: source ONLY the project-level chain (.env -> .${ENV}.env ->
  # .secrets.env from .docker-env-chain), NOT container env_file: paths.
  # `./docker env-files` also appends container env_file: files, and those hold
  # bare compose-interpolation refs like ${DATABASE_DB} that only resolve at
  # compose-merge and are unsafe to shell-source. The requested info variables
  # (COMPOSE_ENV/FILE/PROJECT_NAME/SITE_URL/...) live in the project chain. We
  # read .docker-env-chain directly (same logic as the shim).
  _value_project_chain() {
    if [ -f .docker-env-chain ]; then
      while IFS= read -r _line || [ -n "$_line" ]; do
        case "$_line" in
          ''|\#*) continue ;;
        esac
        _line=$(printf '%s' "$_line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        [ -z "$_line" ] && continue
        printf '%s\n' "$_line" | sed "s|\${ENV}|${_v_env}|g; s|\${COMPOSE_ENV}|${_v_env}|g"
      done < .docker-env-chain
    else
      # Default layout (no .docker-env-chain) — matches the shim.
      printf '%s\n%s\n%s\n' ".env" ".${_v_env}.env" ".secrets.env"
    fi
  }
  _CHAIN_FILES=$(_value_project_chain)

  # Precedence shell > files (like the shim: COMPOSE_ENV from shell > .env, and
  # like compose taking process-env for interpolation). Snapshot the shell value
  # of the requested variable BEFORE sourcing: if it was exported in the
  # environment we return exactly that, not what .env would overwrite. The +x
  # flag distinguishes "set to empty string" from "unset".
  if eval "[ \"\${${VAR_FILTER}+x}\" = x ]"; then
    eval "_SHELL_OVERRIDE=\${${VAR_FILTER}}"
    _HAS_SHELL_OVERRIDE=1
  else
    _HAS_SHELL_OVERRIDE=0
  fi
  (
    # CRITICAL: drop nounset while sourcing — env files contain ${REF} without a
    # value; under set -u that is an "unbound variable" and would crash the
    # subshell (the script runs under set -eu). Without -u an unset ${REF}
    # expands to empty, exactly like compose. set -a exports so later files see
    # values set by earlier ones.
    set +u
    set -a
    _IFS_OLD=$IFS
    IFS='
'
    for _f in $_CHAIN_FILES; do
      [ -f "$_f" ] || continue
      # shellcheck disable=SC1090
      . "$_f"
    done
    IFS=$_IFS_OLD
    set +a
    # Shell override of the requested variable wins over the file value.
    [ "$_HAS_SHELL_OVERRIDE" = 1 ] && eval "${VAR_FILTER}=\${_SHELL_OVERRIDE}"
    # Indirect access to the variable named by --var. eval is safe here:
    # VAR_FILTER is a name from the CLI caller (make), not from env data.
    eval "printf '%s\n' \"\${${VAR_FILTER}:-}\""
  )
  exit 0
fi

GREEN='\033[32m'
DIM='\033[2m'
BOLD='\033[1m'
CYAN='\033[36m'
YELLOW='\033[33m'
RED='\033[31m'
RESET='\033[0m'

_FILE_ENV=$(grep -m1 '^COMPOSE_ENV=' .env 2>/dev/null | cut -d= -f2- || true)
ENV=${COMPOSE_ENV:-${_FILE_ENV:-dev}}

# --- Dynamic discovery (instead of hardcoding) ---
#
# Sources of truth:
#   PROJECT_FILES_ALL    <- ./docker env-files                  (shim prints its chain)
#   KNOWN_SERVICES       <- ./docker compose config --services  (compose knows)
#   container env_file:  <- awk parse of env_file: in docker-compose*.yml

# Project chain: ask the shim (source of truth). It prints absolute paths; we
# convert them to relative-from-PROJECT_DIR for pretty output.
discover_project_chain() {
  ./docker env-files 2>/dev/null | while IFS= read -r abs; do
    [ -z "$abs" ] && continue
    rel=${abs#"$PROJECT_DIR/"}
    printf '%s\n' "$rel"
  done
}

# Layer 1 origin: read .docker-env-chain directly (same logic as the shim),
# return relative paths. No file -> built-in defaults (same as the shim).
discover_layer1_files() {
  if [ -f "${PROJECT_DIR}/.docker-env-chain" ]; then
    while IFS= read -r _line || [ -n "$_line" ]; do
      case "$_line" in ''|\#*) continue ;; esac
      _line=$(printf '%s' "$_line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
      [ -z "$_line" ] && continue
      _line=$(printf '%s' "$_line" | sed "s|\${ENV}|${ENV}|g; s|\${COMPOSE_ENV}|${ENV}|g")
      printf '%s\n' "$_line"
    done < "${PROJECT_DIR}/.docker-env-chain"
  else
    printf '%s\n' ".env"
    printf '%s\n' ".${ENV}.env"
    printf '%s\n' ".secrets.env"
  fi
}

# Services: ask compose. Optional whitelist by presence of env_file:.
discover_services() {
  ./docker compose config --services 2>/dev/null | sort
}

# Container env_file: via the shared parser parse-compose-env-files.sh (single
# source of truth with the engine).
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PARSE_UTIL="$SCRIPT_DIR/parse-compose-env-files.sh"

parse_env_files_for_service() {
  _svc=$1; shift
  [ "$#" -eq 0 ] && return 0
  [ -f "$PARSE_UTIL" ] || { echo "parse-compose-env-files.sh not found: $PARSE_UTIL" >&2; return 1; }
  sh "$PARSE_UTIL" "$ENV" --service "$_svc" "$@"
}

# Cached lookups (avoid repeated shim/docker calls).
PROJECT_CHAIN_CACHE=$(discover_project_chain)
PROJECT_FILES_ALL=$(printf '%s' "$PROJECT_CHAIN_CACHE" | tr '\n' ' ')

# Layer 1 origin set (relative paths, before existence check — the shim skips
# non-existing, but we still want to show them as "missing" in chain output).
LAYER1_FILES=$(discover_layer1_files)

# Compose files within the configured depth, only those declaring env_file:.
COMPOSE_FILES=$(find . -maxdepth "${COMPOSE_DEPTH:-3}" -name 'docker-compose*.yml' -type f 2>/dev/null \
                  | xargs grep -lE '^[[:space:]]+env_file:' 2>/dev/null)

KNOWN_SERVICES=$(discover_services | tr '\n' ' ')

# Layer 2 origin map: "relative_path<TAB>service" per line, built once here.
# One entry per (file, service) pair so files appearing in multiple services
# get both. Iterates all KNOWN_SERVICES to build the complete map.
_build_layer2_tagged() {
  [ -z "$COMPOSE_FILES" ] && return 0
  for _s in $KNOWN_SERVICES; do
    # shellcheck disable=SC2086
    _svc_files=$(sh "$PARSE_UTIL" "$ENV" --service "$_s" $COMPOSE_FILES 2>/dev/null || true)
    [ -z "$_svc_files" ] && continue
    printf '%s\n' "$_svc_files" | while IFS= read -r _abs; do
      [ -z "$_abs" ] && continue
      _rel=${_abs#"$PROJECT_DIR/"}
      printf '%s\t%s\n' "$_rel" "$_s"
    done
  done
}
LAYER2_TAGGED=$(_build_layer2_tagged)

# Resolve --service filter.
if [ -n "$SERVICE" ]; then
  case " $KNOWN_SERVICES " in
    *" $SERVICE "*) ;;
    *) echo "Unknown service '$SERVICE'. Available: $KNOWN_SERVICES" >&2
       exit 2 ;;
  esac
  ACTIVE_SERVICES="$SERVICE"
else
  ACTIVE_SERVICES="$KNOWN_SERVICES"
fi

# --- Helpers ---

filter_existing() {
  for f in "$@"; do [ -f "$f" ] && printf '%s\n' "$f"; done
  return 0
}

# files_for_service <service> -> container env_file: list (space-separated).
files_for_service() {
  _svc=$1
  [ -z "$COMPOSE_FILES" ] && return 0
  # shellcheck disable=SC2086
  parse_env_files_for_service "$_svc" $COMPOSE_FILES | tr '\n' ' '
}

# file_contains_var <file> <varname> -> exit 0 if KEY= present (ignoring comments).
file_contains_var() {
  [ -f "$1" ] || return 1
  grep -qE "^[[:space:]]*$2=" "$1"
}

# origin_tag <relative_file> -> prints "[.docker-env-chain]",
# "[compose env_file: svc]", both, or "" (unknown). Uses pre-built LAYER1_FILES
# (newline list) and LAYER2_TAGGED (file<TAB>svc lines).
origin_tag() {
  _f="$1"
  _tag1="" _tag2=""

  # Layer 1: is this path in the chain file list?
  case "
${LAYER1_FILES}
" in
    *"
${_f}
"*) _tag1="[.docker-env-chain]" ;;
  esac

  # Layer 2: collect all services that list this file.
  if [ -n "$LAYER2_TAGGED" ]; then
    _svcs=$(printf '%s\n' "$LAYER2_TAGGED" \
      | awk -v f="$_f" 'BEGIN{FS="\t"} $1==f {print $2}' \
      | sort -u | tr '\n' ',')
    _svcs=${_svcs%,}   # strip trailing comma
    [ -n "$_svcs" ] && _tag2="[compose env_file: ${_svcs}]"
  fi

  if [ -n "$_tag1" ] && [ -n "$_tag2" ]; then
    printf '%s  %s' "$_tag1" "$_tag2"
  elif [ -n "$_tag1" ]; then
    printf '%s' "$_tag1"
  elif [ -n "$_tag2" ]; then
    printf '%s' "$_tag2"
  fi
}

# --- Header (skip in files/value modes for clean machine-readable output) ---

if [ "$MODE" != "files" ] && [ "$MODE" != "value" ]; then
  PROJECT_NAME=$(basename "$PROJECT_DIR")
  printf "${BOLD}=== env chain — %s (mode: %s" "$PROJECT_NAME" "$MODE"
  [ -n "$SERVICE" ]    && printf ", service=%s" "$SERVICE"
  [ -n "$VAR_FILTER" ] && printf ", var=%s" "$VAR_FILTER"
  printf ") ===${RESET}\n\n"
  printf "  COMPOSE_ENV  = ${CYAN}%s${RESET}" "$ENV"
  [ "${COMPOSE_ENV:-}" = "" ] && printf " ${DIM}(from .env)${RESET}" || printf " ${DIM}(from shell)${RESET}"
  printf "\n"
  printf "  Project dir  = %s\n" "$PROJECT_DIR"
fi

# --- Mode: chain ---

# check_chain <file> [--with-origin]
# Prints the chain line for a project-level file.
# With --with-origin appends a dim origin tag (Layer 1 / Layer 2 / both).
check_chain() {
  f="$1"
  _show_origin="${2:-}"
  marker=""
  if [ -n "$VAR_FILTER" ]; then
    if file_contains_var "$f" "$VAR_FILTER"; then
      marker=" ${YELLOW}[contains ${VAR_FILTER}]${RESET}"
    else
      # skip non-matching files when --var is set
      return 0
    fi
  fi
  _otag=""
  if [ "$_show_origin" = "--with-origin" ]; then
    _otag=$(origin_tag "$f")
    [ -n "$_otag" ] && _otag="  ${DIM}${_otag}${RESET}"
  fi
  if [ -f "$f" ]; then
    if [ -n "$_otag" ]; then
      printf "    ${GREEN}+${RESET} %-30s${_otag}${marker}\n" "$f"
    else
      printf "    ${GREEN}+${RESET} %s${marker}\n" "$f"
    fi
  else
    if [ -z "$VAR_FILTER" ]; then
      if [ -n "$_otag" ]; then
        printf "    ${DIM}·${RESET} ${DIM}%-30s${RESET}${_otag}\n" "$f"
      else
        printf "    ${DIM}·${RESET} ${DIM}%s${RESET}\n" "$f"
      fi
    fi
  fi
}

chain_mode() {
  printf "\n${BOLD}Project-level chain${RESET} ${DIM}(./docker shim → COMPOSE_ENV_FILES, last wins)${RESET}\n\n"
  for f in $PROJECT_FILES_ALL; do check_chain "$f" --with-origin; done

  printf "\n${BOLD}Container env_file:${RESET} ${DIM}(docker compose, runtime)${RESET}\n"
  for svc in $ACTIVE_SERVICES; do
    files=$(files_for_service "$svc")
    [ -z "$files" ] && continue
    printf "\n  ${BOLD}%s${RESET}:\n" "$svc"
    for f in $files; do check_chain "$f"; done
  done
}

# --- Mode: files (raw list, no decoration) ---

files_mode() {
  # The shim already auto-discovers env_file: paths into PROJECT_FILES_ALL;
  # dedupe via awk.
  {
    for f in $PROJECT_FILES_ALL; do
      [ -f "$f" ] || continue
      if [ -n "$VAR_FILTER" ]; then
        file_contains_var "$f" "$VAR_FILTER" || continue
      fi
      printf '%s\n' "$f"
    done
    for svc in $ACTIVE_SERVICES; do
      files=$(files_for_service "$svc")
      [ -z "$files" ] && continue
      for f in $files; do
        [ -f "$f" ] || continue
        if [ -n "$VAR_FILTER" ]; then
          file_contains_var "$f" "$VAR_FILTER" || continue
        fi
        printf '%s\n' "$f"
      done
    done
  } | awk '!seen[$0]++'
}

# --- Mode: diff ---

diff_chain() {
  files=$(filter_existing "$@")
  [ -z "$files" ] && { printf "    ${DIM}(no files)${RESET}\n"; return; }

  # shellcheck disable=SC2086
  awk -v GREEN="$GREEN" -v YELLOW="$YELLOW" -v DIM="$DIM" -v RESET="$RESET" \
      -v VAR_FILTER="$VAR_FILTER" '
    function flush_empty(   _) {
      if (prev_file != "" && !kv_in_file) {
        if (VAR_FILTER != "") printf "      %s(no occurrences of %s)%s\n", DIM, VAR_FILTER, RESET
        else printf "      %s(empty)%s\n", DIM, RESET
      }
    }
    FNR == 1 {
      flush_empty()
      printf "\n  %s\n", FILENAME
      prev_file = FILENAME
      kv_in_file = 0
    }
    {
      line = $0
      sub(/[[:space:]]*#.*$/, "", line)
      sub(/^[[:space:]]+/, "", line)
      sub(/[[:space:]]+$/, "", line)
      if (line == "" || index(line, "=") == 0) next

      key = line
      sub(/=.*/, "", key)
      val = line
      sub(/^[^=]*=/, "", val)

      if (VAR_FILTER != "" && key != VAR_FILTER) next
      kv_in_file = 1

      if (key in seen) {
        if (seen[key] == val) {
          printf "      %s·%s %s%s = %s%s\n", DIM, RESET, DIM, key, val, RESET
        } else {
          printf "      %s~%s %s = %s%s →%s %s\n", YELLOW, RESET, key, seen[key], DIM, RESET, val
        }
      } else {
        printf "      %s+%s %s = %s\n", GREEN, RESET, key, val
      }
      seen[key] = val
    }
    END { flush_empty() }
  ' $files
}

diff_mode() {
  printf "\n${BOLD}Project-level${RESET} ${DIM}(./docker shim)${RESET}\n"
  [ -z "$VAR_FILTER" ] && printf "${DIM}  + new   ~ override   · same value repeated${RESET}\n"
  # shellcheck disable=SC2086
  diff_chain $PROJECT_FILES_ALL

  for svc in $ACTIVE_SERVICES; do
    files=$(files_for_service "$svc")
    [ -z "$files" ] && continue
    printf "\n${BOLD}Container env_file: %s${RESET}\n" "$svc"
    # shellcheck disable=SC2086
    diff_chain $files
  done
}

# --- Mode: effective ---

effective_mode() {
  printf "\n${BOLD}Effective env per service${RESET} ${DIM}(via ./docker compose config)${RESET}\n"

  TMPDIR_=$(mktemp -d)
  trap 'rm -rf "$TMPDIR_"' EXIT
  CFG="$TMPDIR_/config.yml"
  ERR="$TMPDIR_/config.err"

  if ! ./docker compose config > "$CFG" 2> "$ERR"; then
    printf "\n  ${RED}!${RESET} ./docker compose config failed:\n"
    sed 's/^/      /' "$ERR" | head -20
    return 1
  fi

  awk -v BOLD="$BOLD" -v CYAN="$CYAN" -v DIM="$DIM" -v YELLOW="$YELLOW" -v RESET="$RESET" \
      -v SERVICE_FILTER="$SERVICE" -v VAR_FILTER="$VAR_FILTER" '
    function indent(line,   n) {
      n = match(line, /[^ ]/)
      return (n == 0) ? -1 : n - 1
    }
    function service_matches() {
      return (SERVICE_FILTER == "" || current == SERVICE_FILTER)
    }
    { ind = indent($0) }
    /^services:[[:space:]]*$/ { in_services=1; next }
    in_services && ind == 0 && /^[a-z]/ { in_services=0 }
    !in_services { next }

    ind == 2 && /[a-zA-Z][a-zA-Z0-9_-]*:[[:space:]]*$/ {
      current = $0
      sub(/^[[:space:]]+/, "", current)
      sub(/:.*$/, "", current)
      printed = 0
      in_env = 0
      next
    }
    ind == 4 && /^[[:space:]]+environment:[[:space:]]*$/ { in_env=1; next }
    in_env && ind == 4 { in_env=0 }
    in_env && ind == 6 {
      if (!service_matches()) next
      line = $0
      sub(/^[[:space:]]+/, "", line)
      pos = index(line, ":")
      if (pos == 0) next
      key = substr(line, 1, pos-1)
      val = substr(line, pos+2)
      gsub(/^"|"$/, "", val)
      if (VAR_FILTER != "" && key != VAR_FILTER) next
      if (!printed) {
        printf "\n  %s# %s%s\n", BOLD, current, RESET
        printed = 1
        any_output = 1
      }
      printf "      %-30s %s%s%s\n", key, CYAN, val, RESET
    }
    END {
      if (!any_output) {
        if (VAR_FILTER != "" && SERVICE_FILTER != "")
          printf "\n  %s(variable %s not set for service %s)%s\n", DIM, VAR_FILTER, SERVICE_FILTER, RESET
        else if (VAR_FILTER != "")
          printf "\n  %s(variable %s not set in any service)%s\n", DIM, VAR_FILTER, RESET
        else if (SERVICE_FILTER != "")
          printf "\n  %s(service %s has no environment)%s\n", DIM, SERVICE_FILTER, RESET
      }
    }
  ' "$CFG"

  printf "\n${DIM}  Full compose config: %s${RESET}\n" "$CFG"
  trap - EXIT
}

# --- Mode: trace (call stack for one var) ---

# last_def_in_files VAR file1 file2 ... -> "FILE\tLINE\tVALUE_TEMPLATE"
# Last-wins across the file list. Ignores comments. Nothing if not found.
last_def_in_files() {
  _var=$1; shift
  _result=""
  for _f in "$@"; do
    [ -f "$_f" ] || continue
    _match=$(grep -nE "^[[:space:]]*${_var}=" "$_f" 2>/dev/null | tail -1) || true
    if [ -n "$_match" ]; then
      _line=$(printf '%s' "$_match" | cut -d: -f1)
      _val=$(printf '%s' "$_match" | cut -d: -f2-)
      _val=${_val#*=}
      _result="${_f}	${_line}	${_val}"
    fi
  done
  printf '%s' "$_result"
}

# extract_refs VALUE -> newline-separated "NAME\tDEFAULT" pairs.
# Parses ${NAME} and ${NAME:-default} patterns.
extract_refs() {
  printf '%s' "$1" | awk '
    {
      s = $0
      while (match(s, /\$\{[A-Za-z_][A-Za-z0-9_]*(:-[^}]*)?\}/)) {
        chunk = substr(s, RSTART+2, RLENGTH-3)
        name = chunk; def = ""
        pos = index(chunk, ":-")
        if (pos > 0) { name = substr(chunk, 1, pos-1); def = substr(chunk, pos+2) }
        print name "\t" def
        s = substr(s, RSTART + RLENGTH)
      }
    }
  ' | awk '!seen[$0]++'  # dedupe preserving order
}

trace_mode() {
  [ -z "$VAR_FILTER" ] && {
    printf "${RED}!${RESET} --trace requires --var <NAME>.\n  e.g.: %s --trace --var DB_HOST\n" "$0" >&2
    return 2
  }

  # Effective values — via docker compose config.
  TMPDIR_=$(mktemp -d)
  trap 'rm -rf "$TMPDIR_"' EXIT
  CFG="$TMPDIR_/config.yml"
  ERR="$TMPDIR_/config.err"
  if ! ./docker compose config > "$CFG" 2> "$ERR"; then
    printf "\n  ${RED}!${RESET} ./docker compose config failed:\n"
    sed 's/^/      /' "$ERR" | head -10
    return 1
  fi

  _any_traced=0
  for svc in $ACTIVE_SERVICES; do
    _container_files=$(files_for_service "$svc")
    [ -z "$_container_files" ] && continue

    # shellcheck disable=SC2086
    _def=$(last_def_in_files "$VAR_FILTER" $_container_files)
    [ -z "$_def" ] && continue

    _def_file=$(printf '%s' "$_def" | cut -f1)
    _def_line=$(printf '%s' "$_def" | cut -f2)
    _def_val=$(printf '%s'  "$_def" | cut -f3-)
    _eff=$(awk -v svc="$svc" -v var="$VAR_FILTER" '
      function indent(line,   n) { n = match(line, /[^ ]/); return (n == 0) ? -1 : n - 1 }
      { ind = indent($0) }
      /^services:[[:space:]]*$/ { in_services=1; next }
      in_services && ind == 0 && /^[a-z]/ { in_services=0 }
      !in_services { next }
      ind == 2 && /[a-zA-Z][a-zA-Z0-9_-]*:[[:space:]]*$/ {
        current = $0; sub(/^[[:space:]]+/, "", current); sub(/:.*$/, "", current); in_env=0; next
      }
      ind == 4 && /^[[:space:]]+environment:[[:space:]]*$/ { in_env=1; next }
      in_env && ind == 4 { in_env=0 }
      in_env && ind == 6 && current == svc {
        line = $0; sub(/^[[:space:]]+/, "", line)
        pos = index(line, ":"); key = substr(line, 1, pos-1)
        if (key != var) next
        val = substr(line, pos+2); gsub(/^"|"$/, "", val); print val; exit
      }
    ' "$CFG")

    _any_traced=1
    printf "\n${BOLD}=== %s → %s ===${RESET}\n\n" "$VAR_FILTER" "$svc"
    printf "  ${BOLD}[1] Container env_file:${RESET}\n"
    printf "      ${CYAN}%s${RESET}${DIM}:%s${RESET}\n" "$_def_file" "$_def_line"
    printf "         %s${DIM}=${RESET}%s\n\n" "$VAR_FILTER" "$_def_val"

    # Refs from value (one level of project chain).
    _refs=$(extract_refs "$_def_val")
    if [ -n "$_refs" ]; then
      printf "  ${BOLD}[2] References (project chain):${RESET}\n"
      printf '%s\n' "$_refs" | while IFS="	" read -r _ref _default; do
        [ -z "$_ref" ] && continue
        # shellcheck disable=SC2086
        _ref_def=$(last_def_in_files "$_ref" $PROJECT_FILES_ALL)
        printf "      ${CYAN}\${%s}${RESET}\n" "$_ref"
        if [ -n "$_ref_def" ]; then
          _rf=$(printf '%s' "$_ref_def" | cut -f1)
          _rl=$(printf '%s' "$_ref_def" | cut -f2)
          _rv=$(printf '%s' "$_ref_def" | cut -f3-)
          printf "         ${GREEN}+${RESET} %s${DIM}:%s${RESET}  %s${DIM}=${RESET}%s\n" "$_rf" "$_rl" "$_ref" "$_rv"
          # transitive: if _rv itself contains ${...}, flag it.
          case "$_rv" in
            *'${'*) printf "         ${DIM}(transitive: see trace --var %s)${RESET}\n" "$(printf '%s' "$_rv" | sed -n 's/.*\${\([A-Za-z_][A-Za-z0-9_]*\).*/\1/p')" ;;
          esac
        else
          if [ -n "$_default" ]; then
            printf "         ${YELLOW}~${RESET} ${DIM}not set in project chain — fallback default:${RESET} %s\n" "$_default"
          else
            printf "         ${RED}x${RESET} ${DIM}not set, no default — empty string${RESET}\n"
          fi
        fi
      done
      printf "\n"
    else
      printf "  ${DIM}(value has no references — literal)${RESET}\n\n"
    fi

    printf "  ${BOLD}[3] Effective:${RESET}\n"
    if [ -n "$_eff" ]; then
      printf "      %s ${DIM}=${RESET} ${CYAN}%s${RESET}\n" "$VAR_FILTER" "$_eff"
    else
      printf "      ${DIM}(variable does not reach the container env)${RESET}\n"
    fi
  done

  if [ "$_any_traced" = "0" ]; then
    printf "\n  ${DIM}(variable %s not defined in any container env_file for %s)${RESET}\n" "$VAR_FILTER" "${SERVICE:-all services}"
    printf "  ${DIM}Hint: %s --diff --var %s shows where it occurs${RESET}\n" "$0" "$VAR_FILTER"
  fi

  trap - EXIT
}

# --- Dispatch ---

case "$MODE" in
  chain)     chain_mode ;;
  diff)      diff_mode ;;
  effective) effective_mode ;;
  files)     files_mode ;;
  trace)     trace_mode ;;
  value)     : ;;   # handled early (short-circuit before heavy discovery)
esac

# --- Footer (hints) ---

if [ "$MODE" = "chain" ] && [ -z "$VAR_FILTER" ] && [ -z "$SERVICE" ]; then
  printf "\n${DIM}Other modes / filters:${RESET}\n"
  printf "${DIM}  --diff                    what each file changes${RESET}\n"
  printf "${DIM}  --effective               final values per service${RESET}\n"
  printf "${DIM}  --files                   bare list of paths (for grep/xargs)${RESET}\n"
  printf "${DIM}  --service <name>          focus on one container${RESET}\n"
  printf "${DIM}  --var <NAME>              track one variable${RESET}\n"
fi
