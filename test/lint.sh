#!/bin/sh
# ============================================================================
# test/lint.sh — static checks for every shipped shell script.
#
#   sh test/lint.sh
#
# What it does:
#   1. `sh -n` (POSIX syntax check) on every *.sh + bin/docker in the kit.
#   2. shellcheck (POSIX shell dialect) on the same set IF shellcheck is on
#      PATH — gracefully skipped (not failed) when it is absent.
#
# Exit non-zero on the first hard failure (sh -n error or shellcheck error).
# Self-contained: discovers files relative to the kit root, no project state.
# ============================================================================

set -eu

KIT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# --- Collect the scripts we ship --------------------------------------------
# bin/docker has no .sh suffix; the rest are *.sh under lib/, test/, templates/,
# and the top-level install.sh. templates/*.sh (e.g. init.sh) are POSIX scripts
# vendored into target projects and executed, so they must lint too. mk/*.mk and
# completions/* are NOT POSIX sh — skip them.
SCRIPTS=""
add() { [ -f "$1" ] && SCRIPTS="${SCRIPTS}${SCRIPTS:+ }$1"; return 0; }

add "$KIT_DIR/install.sh"
add "$KIT_DIR/bin/docker"
for _f in "$KIT_DIR"/lib/*.sh; do add "$_f"; done
for _f in "$KIT_DIR"/test/*.sh; do add "$_f"; done
for _f in "$KIT_DIR"/templates/*.sh; do add "$_f"; done

if [ -z "$SCRIPTS" ]; then
  printf 'lint: no scripts found under %s\n' "$KIT_DIR" >&2
  exit 1
fi

FAIL=0

# --- 1. sh -n on every script ------------------------------------------------
printf '== sh -n (POSIX syntax) ==\n'
for _s in $SCRIPTS; do
  if sh -n "$_s" 2>/tmp/lint.$$.err; then
    printf '  ok    %s\n' "${_s#"$KIT_DIR"/}"
  else
    printf '  FAIL  %s\n' "${_s#"$KIT_DIR"/}"
    sed 's/^/        /' /tmp/lint.$$.err
    FAIL=1
  fi
done
rm -f /tmp/lint.$$.err

# --- 2. shellcheck (optional) ------------------------------------------------
printf '\n== shellcheck ==\n'
if command -v shellcheck >/dev/null 2>&1; then
  for _s in $SCRIPTS; do
    # -s sh: POSIX dialect (matches our #!/bin/sh contract).
    # -x: follow sourced files where statically resolvable.
    if shellcheck -s sh -x "$_s" >/tmp/lint.$$.sc 2>&1; then
      printf '  ok    %s\n' "${_s#"$KIT_DIR"/}"
    else
      printf '  FAIL  %s\n' "${_s#"$KIT_DIR"/}"
      sed 's/^/        /' /tmp/lint.$$.sc
      FAIL=1
    fi
  done
  rm -f /tmp/lint.$$.sc
else
  printf '  (shellcheck not installed — skipping; install it for deeper checks)\n'
fi

printf '\n'
if [ "$FAIL" -eq 0 ]; then
  printf 'lint: PASS\n'
  exit 0
else
  printf 'lint: FAIL\n'
  exit 1
fi
