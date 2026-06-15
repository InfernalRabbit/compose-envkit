#!/bin/sh
# ============================================================================
# test/smoke.sh — end-to-end integration test for compose-envkit.
#
#   sh test/smoke.sh
#
# Proves the kit's whole reason to exist (Layer-2 env_file: interpolation) plus
# the install flow and every debug mode, in a throwaway project:
#
#   1. mktemp a fresh project dir with a minimal docker-compose.yml whose `web`
#      service declares `env_file: [./svc.env]` and a port interpolation
#      `${SVC_PORT:-0}`. SVC_PORT is defined ONLY in svc.env — nowhere in the
#      project env-chain. Native `docker compose` would NOT see it for
#      interpolation; the kit feeds env_file: paths into COMPOSE_ENV_FILES so it
#      does. (THE acceptance check.)
#   2. `sh install.sh <tmp>` to vendor the kit.
#   3. `./docker compose config` -> ASSERT the resolved published port equals
#      the svc.env value (Layer-2 works).
#   4. `./docker env-files` lists the chain.
#   5. Every env-debug mode runs: --chain --diff --effective --files
#      --trace VAR --value VAR  (and `make env-debug*` if make is present).
#   6. Re-run the shim from a SUBDIR (its own ./docker walks up to scripts/).
#
# Exit non-zero on ANY failure. Requires `docker compose` for steps 3/5; if it
# is absent the Layer-2 assertion cannot run and the test FAILS loudly (rather
# than silently passing) — set SMOKE_SKIP_DOCKER=1 to downgrade that to a skip.
# ============================================================================

set -eu

KIT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

SVC_PORT_VALUE=24680     # distinctive; defined ONLY in svc.env
PORT_DEFAULT=0           # the ${SVC_PORT:-0} fallback we must NOT see

PASS=0
FAIL=0
ok()   { PASS=$((PASS+1)); printf '  PASS  %s\n' "$*"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL  %s\n' "$*"; }
info() { printf '  ..    %s\n' "$*"; }

# --- Workspace ---------------------------------------------------------------
WORK=$(mktemp -d 2>/dev/null || mktemp -d -t composeenvkit)
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM
# Resolve symlinks (macOS mktemp lives under /var -> /private/var) so paths
# compare cleanly against $PWD later.
WORK=$(CDPATH= cd -- "$WORK" && pwd)

printf '== compose-envkit smoke test ==\n'
printf '  kit:  %s\n' "$KIT_DIR"
printf '  work: %s\n\n' "$WORK"

# --- 1. Minimal project ------------------------------------------------------
printf '[1] scaffold a minimal project\n'

cat > "$WORK/docker-compose.yml" <<YAML
services:
  web:
    image: busybox
    env_file:
      - ./svc.env
    ports:
      - "\${SVC_PORT:-${PORT_DEFAULT}}:80"
    environment:
      SVC_PORT: "\${SVC_PORT:-${PORT_DEFAULT}}"
YAML

# SVC_PORT lives ONLY here — never in the project chain (.env/.dev.env/...).
printf 'SVC_PORT=%s\n' "$SVC_PORT_VALUE" > "$WORK/svc.env"

# A Makefile so we can exercise the make targets too.
cat > "$WORK/Makefile" <<'MK'
include scripts/compose.mk
MK

# A subdir to test the "run from a subproject" path.
mkdir -p "$WORK/sub"

ok "project scaffolded (docker-compose.yml, svc.env, Makefile, sub/)"

# --- 2. Install --------------------------------------------------------------
printf '\n[2] run install.sh into the temp project\n'
if sh "$KIT_DIR/install.sh" "$WORK" >"$WORK/.install.log" 2>&1; then
  ok "install.sh exited 0"
else
  bad "install.sh failed:"; sed 's/^/        /' "$WORK/.install.log"
fi

# Verify the install layout contract.
for _p in \
  "$WORK/docker" \
  "$WORK/scripts/compose-env.sh" \
  "$WORK/scripts/parse-compose-env-files.sh" \
  "$WORK/scripts/env-debug.sh" \
  "$WORK/scripts/compose.mk" \
  "$WORK/scripts/env-debug.mk" \
  "$WORK/.docker-env-chain"; do
  if [ -e "$_p" ]; then ok "vendored: ${_p#"$WORK"/}"
  else bad "missing after install: ${_p#"$WORK"/}"; fi
done

if [ -x "$WORK/docker" ]; then ok "./docker is executable"
else bad "./docker is not executable"; fi

# install must NOT clobber a pre-existing real .env: write one, re-run, check.
printf 'PRESERVE_ME=1\n' > "$WORK/.env"
sh "$KIT_DIR/install.sh" "$WORK" >/dev/null 2>&1 || true
if grep -q '^PRESERVE_ME=1$' "$WORK/.env" 2>/dev/null; then
  ok "idempotent re-install preserved an existing .env"
else
  bad "re-install clobbered an existing .env"
fi
# Keep COMPOSE_ENV out of the way for deterministic discovery (default dev).
printf 'PRESERVE_ME=1\nCOMPOSE_ENV=dev\n' > "$WORK/.env"

# --- Helper to run the shim from a given dir --------------------------------
# We cd into the dir (the shim self-locates from $0's dir) and run ./docker.
run_shim() {  # run_shim <dir> <args...>
  _d=$1; shift
  ( CDPATH= cd -- "$_d" && ./docker "$@" )
}

# --- docker availability gate ------------------------------------------------
HAVE_DOCKER=0
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  HAVE_DOCKER=1
fi

# ============================================================================
# 3. THE acceptance check — Layer-2 env_file: interpolation
# ============================================================================
printf '\n[3] Layer-2: ./docker compose config resolves ${SVC_PORT} from svc.env\n'

assert_layer2() {  # assert_layer2 <run-dir> <label>
  _dir=$1; _label=$2
  _cfg=$(run_shim "$_dir" compose config 2>"$WORK/.cfg.err") || {
    bad "[$_label] ./docker compose config failed:"
    sed 's/^/        /' "$WORK/.cfg.err"
    return 1
  }
  # Look for the resolved port. compose renders `published: "24680"` and also
  # `SVC_PORT: "24680"` in environment. Either proves interpolation pulled the
  # value from svc.env via COMPOSE_ENV_FILES.
  if printf '%s\n' "$_cfg" | grep -q "$SVC_PORT_VALUE"; then
    ok "[$_label] resolved port == svc.env value ($SVC_PORT_VALUE)"
  else
    bad "[$_label] expected $SVC_PORT_VALUE not found in compose config"
    printf '%s\n' "$_cfg" | sed 's/^/        /' | head -30
    return 1
  fi
  # Negative: the :-0 fallback must NOT be what published resolves to.
  if printf '%s\n' "$_cfg" | grep -E "published:[[:space:]]*\"?${PORT_DEFAULT}\"?[[:space:]]*$" >/dev/null 2>&1; then
    bad "[$_label] published resolved to the :- fallback ($PORT_DEFAULT) — Layer-2 did NOT fire"
    return 1
  fi
  return 0
}

if [ "$HAVE_DOCKER" -eq 1 ]; then
  assert_layer2 "$WORK" "root" || true
else
  if [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
    info "docker compose unavailable + SMOKE_SKIP_DOCKER=1 — skipping Layer-2 assertion"
  else
    bad "docker compose unavailable — cannot prove Layer-2 (set SMOKE_SKIP_DOCKER=1 to skip)"
  fi
fi

# --- 4. ./docker env-files ---------------------------------------------------
printf '\n[4] ./docker env-files lists the resolved chain\n'
_files=$(run_shim "$WORK" env-files 2>"$WORK/.ef.err" || true)
if [ -n "$_files" ]; then
  # svc.env is an env_file: path — Layer-2 auto-discovery must include it.
  if printf '%s\n' "$_files" | grep -q 'svc.env$'; then
    ok "env-files includes the auto-discovered svc.env (Layer-2 discovery)"
  else
    bad "env-files did not include svc.env"
    printf '%s\n' "$_files" | sed 's/^/        /'
  fi
  # .env is Layer-1 and the chain lists it unconditionally — its absence is a
  # regression (a total Layer-1 loss), so fail rather than info.
  if printf '%s\n' "$_files" | grep -q '/\.env$'; then
    ok "env-files includes Layer-1 .env"
  else
    bad "env-files did NOT list .env (Layer-1 lost):"
    printf '%s\n' "$_files" | sed 's/^/        /'
  fi
else
  bad "env-files produced no output:"
  sed 's/^/        /' "$WORK/.ef.err" 2>/dev/null || true
fi

# --- 5. Every env-debug mode -------------------------------------------------
printf '\n[5] env-debug modes\n'
EDBG="$WORK/scripts/env-debug.sh"

run_edbg() {  # run_edbg <label> <args...>; asserts exit 0
  _label=$1; shift
  if ( CDPATH= cd -- "$WORK" && sh "$EDBG" "$@" ) >"$WORK/.edbg.out" 2>"$WORK/.edbg.err"; then
    ok "env-debug $_label"
  else
    bad "env-debug $_label (exit $?):"
    sed 's/^/        /' "$WORK/.edbg.err" 2>/dev/null | head -10
  fi
}

if [ "$HAVE_DOCKER" -eq 1 ]; then
  run_edbg "--chain"               --chain
  run_edbg "--diff"                --diff
  run_edbg "--effective"           --effective
  run_edbg "--files"               --files
  run_edbg "--trace --var SVC_PORT" --trace --var SVC_PORT
else
  info "docker unavailable — chain/diff/effective/files/trace need ./docker compose; skipped"
fi

# --value sources ONLY the project chain (no docker needed). Put a known var in
# the chain and assert env-debug --value returns it.
printf 'SMOKE_VAL=hello-layer1\n' >> "$WORK/.env"
_val=$( ( CDPATH= cd -- "$WORK" && sh "$EDBG" --value --var SMOKE_VAL ) 2>"$WORK/.val.err" || true )
if [ "$_val" = "hello-layer1" ]; then
  ok "env-debug --value --var SMOKE_VAL == 'hello-layer1'"
else
  bad "env-debug --value returned '$_val' (expected 'hello-layer1')"
  sed 's/^/        /' "$WORK/.val.err" 2>/dev/null | head -5
fi

# env-debug --value precedence: ${VAR:-default} resolves a default when unset.
_val2=$( ( CDPATH= cd -- "$WORK" && sh "$EDBG" --value --var DEFINITELY_UNSET ) 2>/dev/null || true )
if [ -z "$_val2" ]; then
  ok "env-debug --value on an unset var yields empty (no crash under set -u)"
else
  bad "env-debug --value on unset var returned '$_val2' (expected empty)"
fi

# --- 6. make targets (if make present) --------------------------------------
printf '\n[6] make env-debug* targets\n'
if command -v make >/dev/null 2>&1 && [ "$HAVE_DOCKER" -eq 1 ]; then
  for _t in env-debug env-debug-diff env-debug-effective env-debug-files; do
    if ( CDPATH= cd -- "$WORK" && make -s "$_t" ) >/dev/null 2>"$WORK/.mk.err"; then
      ok "make $_t"
    else
      bad "make $_t:"; sed 's/^/        /' "$WORK/.mk.err" | head -8
    fi
  done
  if ( CDPATH= cd -- "$WORK" && make -s env-debug-trace VAR=SVC_PORT ) >/dev/null 2>"$WORK/.mk.err"; then
    ok "make env-debug-trace VAR=SVC_PORT"
  else
    bad "make env-debug-trace:"; sed 's/^/        /' "$WORK/.mk.err" | head -8
  fi
  if ( CDPATH= cd -- "$WORK" && make -s validate ) >/dev/null 2>"$WORK/.mk.err"; then
    ok "make validate (compose config check)"
  else
    bad "make validate:"; sed 's/^/        /' "$WORK/.mk.err" | head -8
  fi
else
  info "make and/or docker unavailable — skipping make-target checks"
fi

# --- 7. Run from a subdir (subproject path) ---------------------------------
printf '\n[7] run from a subdir — its own ./docker walks up to scripts/\n'
# Drop a copy of the shim into sub/ (the documented subproject pattern).
cp "$WORK/docker" "$WORK/sub/docker"
chmod +x "$WORK/sub/docker"
# Give sub/ its OWN compose + svc.env so its self-located run is self-contained.
cat > "$WORK/sub/docker-compose.yml" <<YAML
services:
  api:
    image: busybox
    env_file:
      - ./svc.env
    environment:
      SVC_PORT: "\${SVC_PORT:-${PORT_DEFAULT}}"
YAML
printf 'SVC_PORT=%s\n' "$SVC_PORT_VALUE" > "$WORK/sub/svc.env"

# env-files from the subdir must resolve against sub/ (its own svc.env). The
# engine may emit a path with a harmless `./` segment (compose declares
# `env_file: ./svc.env`), so collapse `/./` before matching and confirm at
# least one listed path actually points at sub/svc.env on disk.
_subfiles=$(run_shim "$WORK/sub" env-files 2>"$WORK/.sub.err" || true)
_sub_hit=0
# Collapse any `/./` segment, then look for the exact sub/svc.env path that
# also exists on disk. Loop over a here-string-free read of the captured list.
_oldIFS=$IFS; IFS='
'
for _p in $_subfiles; do
  [ -n "$_p" ] || continue
  _norm=$(printf '%s\n' "$_p" | sed 's|/\./|/|g')
  if [ "$_norm" = "$WORK/sub/svc.env" ] && [ -f "$_p" ]; then
    _sub_hit=1
    break
  fi
done
IFS=$_oldIFS
if [ "$_sub_hit" = 1 ]; then
  ok "subdir ./docker env-files resolves sub/svc.env (self-located)"
else
  bad "subdir ./docker env-files did not resolve sub/svc.env:"
  printf '%s\n' "$_subfiles" | sed 's/^/        /'
fi

if [ "$HAVE_DOCKER" -eq 1 ]; then
  assert_layer2 "$WORK/sub" "subdir" || true
else
  info "docker unavailable — skipping subdir Layer-2 assertion"
fi

# --- Summary -----------------------------------------------------------------
printf '\n== summary ==\n'
printf '  passed: %s\n' "$PASS"
printf '  failed: %s\n' "$FAIL"
if [ "$FAIL" -eq 0 ]; then
  printf '\nsmoke: PASS\n'
  exit 0
else
  printf '\nsmoke: FAIL\n'
  exit 1
fi
