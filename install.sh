#!/bin/sh
# ============================================================================
# compose-envkit installer — vendor the kit into a target project.
#
#   sh install.sh [target-dir]      (default: current directory)
#
# What it does (idempotent):
#   1. Vendor  lib/*.sh  +  mk/*.mk  ->  <target>/scripts/
#      (flattened: env files and make files land side-by-side, so the make
#       includes and the ./docker shim find them by plain name under scripts/).
#   2. Vendor  completions/*         ->  <target>/scripts/completions/
#   3. Copy    bin/docker            ->  <target>/docker   (chmod +x)
#   4. Generate <target>/.docker-env-chain from templates/docker-env-chain
#      (only if absent — never clobber a project's real chain).
#   5. Generate <target>/example.* from templates/example.*
#      (only if absent). NEVER touches a real .env / .secrets.env.
#   6. Generate <target>/init.sh from templates/init.sh (only if absent; +x).
#      A customizable one-time bootstrap: seeds env files from example.* and
#      fans out to subproject init.sh scripts. No sudo / chmod 777 / secrets.
#   7. Print the Makefile `include scripts/compose.mk` snippet + next steps.
#
# Layout produced in the target (the "install layout contract"):
#   <target>/docker                       <- self-locating shim
#   <target>/scripts/compose-env.sh       <- COMPOSE_ENV_FILES assembly
#   <target>/scripts/parse-compose-env-files.sh
#   <target>/scripts/env-debug.sh
#   <target>/scripts/compose.mk           <- `include scripts/compose.mk`
#   <target>/scripts/env-debug.mk
#   <target>/scripts/completions/*
#   <target>/.docker-env-chain            <- generated if missing
#   <target>/example.*                    <- generated if missing
#   <target>/init.sh                      <- generated if missing (bootstrap, +x)
#
# Flags:
#   --help, -h     show this help and exit
#   --dry-run, -n  print every action without writing anything
#
# Subproject case: a subproject inside an already-installed repo can either
#   (a) rely on the parent (`../scripts/...`) — just drop a copy of bin/docker
#       as <subproject>/docker; the shim walks up to the parent's scripts/, OR
#   (b) be fully self-contained — run `sh install.sh <subproject-dir>` to
#       vendor its own scripts/ copy (works when cloned standalone).
# The shipped bin/docker self-locates either way (own scripts/ -> parent's).
# ============================================================================

set -eu

# --- Resolve where this installer (and the kit payload) live -----------------
# Portable: no readlink -f / realpath. CDPATH= guards against cd echoing.
KIT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

DRY_RUN=0
TARGET_ARG=""

print_help() {
  # Reprint the banner block above (lines up to the first non-comment line).
  awk '
    NR == 1 { next }                       # skip shebang
    /^#/    { sub(/^# ?/, ""); print; next }
    { exit }
  ' "$0"
}

# --- Parse args --------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)    print_help; exit 0 ;;
    --dry-run|-n) DRY_RUN=1 ;;
    --)           shift; break ;;
    -*)           printf 'install.sh: unknown option: %s\n' "$1" >&2
                  printf 'Run: sh install.sh --help\n' >&2
                  exit 2 ;;
    *)            if [ -n "$TARGET_ARG" ]; then
                    printf 'install.sh: too many arguments (target already "%s")\n' "$TARGET_ARG" >&2
                    exit 2
                  fi
                  TARGET_ARG="$1" ;;
  esac
  shift
done
# A trailing positional after `--`
if [ $# -gt 0 ] && [ -z "$TARGET_ARG" ]; then
  TARGET_ARG="$1"
fi

# --- Resolve target ----------------------------------------------------------
TARGET_ARG=${TARGET_ARG:-.}
if [ ! -d "$TARGET_ARG" ]; then
  if [ "$DRY_RUN" -eq 1 ]; then
    # In dry-run, tolerate a not-yet-existing target so the plan still prints.
    printf '%s\n' "[dry-run] target '$TARGET_ARG' does not exist yet — would create it" >&2
    TARGET_DIR=$TARGET_ARG
  else
    printf 'install.sh: target directory does not exist: %s\n' "$TARGET_ARG" >&2
    printf '  create it first (mkdir -p "%s") or pass an existing dir.\n' "$TARGET_ARG" >&2
    exit 2
  fi
else
  TARGET_DIR=$(CDPATH= cd -- "$TARGET_ARG" && pwd)
fi

# Guard: refuse to install the kit into itself (would self-overwrite).
if [ "$TARGET_DIR" = "$KIT_DIR" ]; then
  printf 'install.sh: refusing to install the kit into its own source dir.\n' >&2
  printf '  Pass a target project dir: sh install.sh /path/to/project\n' >&2
  exit 2
fi

SCRIPTS_DIR="$TARGET_DIR/scripts"

# --- Action helpers (honor --dry-run) ----------------------------------------
say()  { printf '%s\n' "$*"; }
note() { printf '  %s\n' "$*"; }

do_mkdir() {
  # $1 = dir
  if [ -d "$1" ]; then
    return 0
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    note "mkdir -p $1"
  else
    mkdir -p "$1"
  fi
}

# do_copy SRC DST [--exec]
# Always overwrites (vendored payload is owned by the kit). Optional +x.
do_copy() {
  _src=$1; _dst=$2; _exec=${3:-}
  if [ "$DRY_RUN" -eq 1 ]; then
    note "copy   $(basename "$_src")  ->  ${_dst#"$TARGET_DIR"/}"
    [ "$_exec" = "--exec" ] && note "chmod +x ${_dst#"$TARGET_DIR"/}"
    return 0
  fi
  cp "$_src" "$_dst"
  [ "$_exec" = "--exec" ] && chmod +x "$_dst"
  return 0
}

# do_generate SRC DST [--exec]
# Copies a template ONLY if the destination is absent (never clobber). With
# --exec, chmod +x the freshly created file. Returns 0 if it created the file,
# 1 if it skipped an existing one.
do_generate() {
  _src=$1; _dst=$2; _exec=${3:-}
  if [ -e "$_dst" ]; then
    note "skip   ${_dst#"$TARGET_DIR"/}  (exists — left untouched)"
    return 1
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    note "create ${_dst#"$TARGET_DIR"/}  (from templates/$(basename "$_src"))"
    [ "$_exec" = "--exec" ] && note "chmod +x ${_dst#"$TARGET_DIR"/}"
  else
    cp "$_src" "$_dst"
    [ "$_exec" = "--exec" ] && chmod +x "$_dst"
  fi
  return 0
}

# --- Sanity: the kit payload must be present --------------------------------
for _need in "$KIT_DIR/lib" "$KIT_DIR/mk" "$KIT_DIR/bin/docker" "$KIT_DIR/templates"; do
  if [ ! -e "$_need" ]; then
    printf 'install.sh: kit payload missing: %s\n' "$_need" >&2
    printf '  Run from a complete compose-envkit checkout.\n' >&2
    exit 1
  fi
done

say ""
say "compose-envkit installer"
say "  kit:    $KIT_DIR"
say "  target: $TARGET_DIR"
[ "$DRY_RUN" -eq 1 ] && say "  mode:   DRY RUN (no files written)"
say ""

# --- 1. Vendor lib/*.sh + mk/*.mk into target/scripts/ ----------------------
say "[1/6] vendor lib + mk -> scripts/"
do_mkdir "$SCRIPTS_DIR"

for _f in "$KIT_DIR"/lib/*.sh; do
  [ -e "$_f" ] || continue
  _dst="$SCRIPTS_DIR/$(basename "$_f")"
  do_copy "$_f" "$_dst" --exec
done

for _f in "$KIT_DIR"/mk/*.mk; do
  [ -e "$_f" ] || continue
  _dst="$SCRIPTS_DIR/$(basename "$_f")"
  do_copy "$_f" "$_dst"
done

# --- 2. Vendor completions (optional) ---------------------------------------
if [ -d "$KIT_DIR/completions" ]; then
  _have_compl=0
  for _f in "$KIT_DIR"/completions/*; do
    [ -e "$_f" ] || continue
    _have_compl=1
    break
  done
  if [ "$_have_compl" -eq 1 ]; then
    say "[2/6] vendor completions -> scripts/completions/"
    do_mkdir "$SCRIPTS_DIR/completions"
    for _f in "$KIT_DIR"/completions/*; do
      [ -e "$_f" ] || continue
      do_copy "$_f" "$SCRIPTS_DIR/completions/$(basename "$_f")"
    done
  else
    say "[2/6] completions: none shipped — skipping"
  fi
else
  say "[2/6] completions: none shipped — skipping"
fi

# --- 3. Copy bin/docker -> target/docker ------------------------------------
say "[3/6] install ./docker shim"
do_copy "$KIT_DIR/bin/docker" "$TARGET_DIR/docker" --exec

# --- 4. Generate .docker-env-chain (never clobber) --------------------------
say "[4/6] generate .docker-env-chain"
if [ -f "$KIT_DIR/templates/docker-env-chain" ]; then
  do_generate "$KIT_DIR/templates/docker-env-chain" "$TARGET_DIR/.docker-env-chain" || true
else
  note "skip   templates/docker-env-chain not found in kit"
fi

# --- 5. Generate example.* (never clobber; never touch real .env) -----------
say "[5/6] generate example.* templates"
_gen_any=0
for _tpl in "$KIT_DIR"/templates/example.*; do
  [ -e "$_tpl" ] || continue
  _name=$(basename "$_tpl")
  do_generate "$_tpl" "$TARGET_DIR/$_name" && _gen_any=1
done
[ "$_gen_any" -eq 0 ] && note "(no new example.* templates created)"

# Defensive note: we generate example.* but never .env/.secrets.env directly.
# Those are produced by the user (cp example.env .env) and stay gitignored.

# --- 6. Generate init.sh (never clobber; chmod +x) --------------------------
say "[6/6] generate init.sh"
if [ -f "$KIT_DIR/templates/init.sh" ]; then
  do_generate "$KIT_DIR/templates/init.sh" "$TARGET_DIR/init.sh" --exec || true
else
  note "skip   templates/init.sh not found in kit"
fi

# --- Completion hint ---------------------------------------------------------
say ""
say "Done."
say ""
say "Next steps"
say "----------"
say "1. Add this line to your target Makefile (once):"
say ""
say "       include scripts/compose.mk"
say ""
say "   It pulls in DC / DC_PROD / PLATFORM / validate / help and the"
say "   env-debug* targets (via a transitive include of scripts/env-debug.mk)."
say ""
say "2. Create your real env files from the templates:"
say ""
say "       ./init.sh                            # seed every .X from example.X"
say "       # …or by hand:"
say "       cp example.env         .env"
say "       cp example.secrets.env .secrets.env   # fill secrets, stays gitignored"
say ""
say "3. Tab-completion for make env-debug-* targets:"
say ""
say "       make install-completions     # prints bash/zsh source lines"
say ""
say "4. Try it:"
say ""
say "       ./docker env-files           # the resolved env chain"
say "       ./docker compose config      # interpolation with env_file: layer"
say "       make env-debug               # inspect the chain"
say ""
say "Subprojects: drop a copy of ./docker into the subproject dir — it walks up"
say "to the parent scripts/. For a standalone-cloneable subproject, run"
say "'sh install.sh <subproject-dir>' to vendor its own scripts/ copy."
say ""

exit 0
