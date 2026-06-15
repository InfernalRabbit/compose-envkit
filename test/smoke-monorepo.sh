#!/bin/sh
# ============================================================================
# test/smoke-monorepo.sh — end-to-end test for the MONOREPO topology.
#
#   sh test/smoke-monorepo.sh
#
# Proves the root-orchestrates-subprojects pattern that examples/monorepo/
# blueprints: a root docker-compose.yml `include:`s two subprojects (web/, api/),
# each with a service `env_file:` whose port var lives ONLY there. The kit's
# depth-N Layer-2 discovery (COMPOSE_DEPTH, default 3) reaches across the
# include from the ROOT, so both subproject ports resolve in the unified config.
#
# Steps:
#   1. Copy examples/monorepo/ into a fresh mktemp dir, `install.sh` into it,
#      create the real root .env from the template.
#   2. NATIVE baseline — `docker compose config` WITHOUT the kit from the root:
#      ASSERT both ports fall back to 0 (the gap the kit closes).
#   3. ROOT + kit — `./docker compose config`: ASSERT WEB_PORT==18080 AND
#      API_PORT==19090 (cross-subproject Layer-2 via the include), and neither
#      published port is the :-0 fallback.
#   4. `./docker env-files` from the root lists BOTH web/.web.env and
#      api/.api.env (Layer-2 discovery found them across the include).
#   5. ISOLATED subproject (Option A shim) — drop ./docker into web/ and assert
#      it resolves its OWN WEB_PORT independent of the root.
#
# Exit non-zero on ANY failure. Steps 2/3/5 need `docker compose`; if it is
# absent the cross-subproject Layer-2 assertion cannot run and the test FAILS
# loudly — set SMOKE_SKIP_DOCKER=1 to downgrade the docker-dependent checks to
# skips (the env-files discovery check in step 4 still runs).
# ============================================================================

set -eu

KIT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BLUEPRINT="$KIT_DIR/examples/monorepo"

WEB_PORT_VALUE=18080     # defined ONLY in web/.web.env
API_PORT_VALUE=19090     # defined ONLY in api/.api.env
PORT_DEFAULT=0           # the ${*_PORT:-0} fallback native compose lands on

PASS=0
FAIL=0
ok()   { PASS=$((PASS+1)); printf '  PASS  %s\n' "$*"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL  %s\n' "$*"; }
info() { printf '  ..    %s\n' "$*"; }

# --- Sanity: the blueprint must exist ---------------------------------------
if [ ! -d "$BLUEPRINT" ]; then
  printf 'smoke-monorepo: blueprint missing: %s\n' "$BLUEPRINT" >&2
  exit 1
fi

# --- Workspace ---------------------------------------------------------------
WORK=$(mktemp -d 2>/dev/null || mktemp -d -t composeenvkitmono)
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM
# Resolve symlinks (macOS mktemp lives under /var -> /private/var) so paths
# compare cleanly against the listed env-files later.
WORK=$(CDPATH= cd -- "$WORK" && pwd)

printf '== compose-envkit MONOREPO smoke test ==\n'
printf '  kit:       %s\n' "$KIT_DIR"
printf '  blueprint: %s\n' "$BLUEPRINT"
printf '  work:      %s\n\n' "$WORK"

# --- 1. Copy the blueprint + install ----------------------------------------
printf '[1] copy examples/monorepo/ and install.sh into a temp dir\n'

# Copy the blueprint's contents (including dotfiles) into WORK.
cp -R "$BLUEPRINT/." "$WORK/"
ok "blueprint copied (root + web/ + api/)"

if sh "$KIT_DIR/install.sh" "$WORK" >"$WORK/.install.log" 2>&1; then
  ok "install.sh exited 0"
else
  bad "install.sh failed:"; sed 's/^/        /' "$WORK/.install.log"
fi

# The shipped .docker-env-chain / example.env must NOT be clobbered by install.
if grep -q '^COMPOSE_PROJECT_NAME=monorepo$' "$WORK/example.env" 2>/dev/null; then
  ok "install.sh preserved the blueprint's example.env"
else
  bad "install.sh clobbered the blueprint's example.env"
fi

# Real root env from the template (non-secret). COMPOSE_ENV stays dev.
cp "$WORK/example.env" "$WORK/.env"

for _p in \
  "$WORK/docker" \
  "$WORK/scripts/compose-env.sh" \
  "$WORK/scripts/parse-compose-env-files.sh" \
  "$WORK/.docker-env-chain" \
  "$WORK/web/docker-compose.yml" \
  "$WORK/web/.web.env" \
  "$WORK/api/docker-compose.yml" \
  "$WORK/api/.api.env"; do
  if [ -e "$_p" ]; then ok "present: ${_p#"$WORK"/}"
  else bad "missing: ${_p#"$WORK"/}"; fi
done

# --- Helper to run the shim from a given dir --------------------------------
run_shim() {  # run_shim <dir> <args...>
  _d=$1; shift
  ( CDPATH= cd -- "$_d" && ./docker "$@" )
}

# --- docker availability gate ------------------------------------------------
HAVE_DOCKER=0
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  HAVE_DOCKER=1
fi

# Extract the value of `KEY: "<value>"` from a compose-config rendering.
config_value() {  # config_value <config-text> <KEY>
  printf '%s\n' "$1" \
    | grep -E "^[[:space:]]*$2:[[:space:]]*\"?[0-9]+\"?[[:space:]]*$" \
    | head -1 \
    | sed -E "s/^[[:space:]]*$2:[[:space:]]*\"?([0-9]+)\"?[[:space:]]*$/\1/"
}

# ============================================================================
# 2. NATIVE baseline — prove the gap (no kit): ports fall back to 0
# ============================================================================
printf '\n[2] NATIVE baseline: raw `docker compose config` from the root\n'
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _native=$( ( CDPATH= cd -- "$WORK" && docker compose config ) 2>"$WORK/.native.err" ) || {
    bad "native docker compose config failed (blueprint should be valid):"
    sed 's/^/        /' "$WORK/.native.err"
    _native=""
  }
  if [ -n "$_native" ]; then
    _nweb=$(config_value "$_native" WEB_PORT)
    _napi=$(config_value "$_native" API_PORT)
    if [ "$_nweb" = "$PORT_DEFAULT" ] && [ "$_napi" = "$PORT_DEFAULT" ]; then
      ok "native config shows the :-0 fallback for BOTH ports (WEB=$_nweb API=$_napi) — the gap"
    else
      bad "native config did NOT show the fallback (WEB=$_nweb API=$_napi); expected both $PORT_DEFAULT"
    fi
  fi
else
  if [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
    info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping native baseline"
  else
    bad "docker compose unavailable — cannot show native baseline (set SMOKE_SKIP_DOCKER=1 to skip)"
  fi
fi

# ============================================================================
# 3. THE acceptance check — cross-subproject Layer-2 from the ROOT
# ============================================================================
printf '\n[3] ROOT + kit: ./docker compose config resolves BOTH subproject ports\n'
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _cfg=$(run_shim "$WORK" compose config 2>"$WORK/.cfg.err") || {
    bad "root ./docker compose config failed:"
    sed 's/^/        /' "$WORK/.cfg.err"
    _cfg=""
  }
  if [ -n "$_cfg" ]; then
    _web=$(config_value "$_cfg" WEB_PORT)
    _api=$(config_value "$_cfg" API_PORT)
    if [ "$_web" = "$WEB_PORT_VALUE" ]; then
      ok "WEB_PORT resolved to web/.web.env value ($WEB_PORT_VALUE) — Layer-2 across include"
    else
      bad "WEB_PORT == '$_web' (expected $WEB_PORT_VALUE)"
      printf '%s\n' "$_cfg" | grep -E 'WEB_PORT|published' | sed 's/^/        /'
    fi
    if [ "$_api" = "$API_PORT_VALUE" ]; then
      ok "API_PORT resolved to api/.api.env value ($API_PORT_VALUE) — Layer-2 across include"
    else
      bad "API_PORT == '$_api' (expected $API_PORT_VALUE)"
      printf '%s\n' "$_cfg" | grep -E 'API_PORT|published' | sed 's/^/        /'
    fi
    # Negative: neither published port may be the :-0 fallback.
    if printf '%s\n' "$_cfg" | grep -E "published:[[:space:]]*\"?${PORT_DEFAULT}\"?[[:space:]]*$" >/dev/null 2>&1; then
      bad "a published port resolved to the :-0 fallback — Layer-2 did NOT fully fire"
    else
      ok "no published port fell back to :-0 (both subprojects resolved)"
    fi
  fi
else
  if [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
    info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping cross-subproject Layer-2 assertion"
  else
    bad "docker compose unavailable — cannot prove cross-subproject Layer-2 (set SMOKE_SKIP_DOCKER=1 to skip)"
  fi
fi

# ============================================================================
# 4. ./docker env-files from the root lists BOTH subproject env files
# ============================================================================
printf '\n[4] ROOT ./docker env-files lists web/.web.env AND api/.api.env\n'
# This does NOT need docker — the chain is assembled by the engine's awk parser.
_files=$(run_shim "$WORK" env-files 2>"$WORK/.ef.err" || true)
if [ -n "$_files" ]; then
  # The engine may emit a harmless `/./` segment (compose declares
  # `env_file: ./.web.env`); collapse it before matching, like smoke.sh does.
  _web_hit=0
  _api_hit=0
  _oldIFS=$IFS; IFS='
'
  for _p in $_files; do
    [ -n "$_p" ] || continue
    _norm=$(printf '%s\n' "$_p" | sed 's|/\./|/|g')
    [ "$_norm" = "$WORK/web/.web.env" ] && [ -f "$_p" ] && _web_hit=1
    [ "$_norm" = "$WORK/api/.api.env" ] && [ -f "$_p" ] && _api_hit=1
  done
  IFS=$_oldIFS
  if [ "$_web_hit" = 1 ]; then
    ok "env-files includes web/.web.env (cross-subproject Layer-2 discovery)"
  else
    bad "env-files did NOT include web/.web.env:"
    printf '%s\n' "$_files" | sed 's/^/        /'
  fi
  if [ "$_api_hit" = 1 ]; then
    ok "env-files includes api/.api.env (cross-subproject Layer-2 discovery)"
  else
    bad "env-files did NOT include api/.api.env:"
    printf '%s\n' "$_files" | sed 's/^/        /'
  fi
  # Layer-1 root .env must be present too.
  if printf '%s\n' "$_files" | grep -q '/\.env$'; then
    ok "env-files includes the Layer-1 root .env"
  else
    info "env-files did not list the root .env (ok if the chain omits it)"
  fi
else
  bad "env-files produced no output:"
  sed 's/^/        /' "$WORK/.ef.err" 2>/dev/null || true
fi

# ============================================================================
# 5. ISOLATED subproject (Option A) — web/ resolves its OWN port
# ============================================================================
printf '\n[5] ISOLATED web/ (Option A shim) resolves its own WEB_PORT\n'
# Drop a copy of the root shim into web/ — it walks up to the root scripts/.
cp "$WORK/docker" "$WORK/web/docker"
chmod +x "$WORK/web/docker"

# env-files from web/ must resolve against web/ (its own .web.env), not the root.
_subfiles=$(run_shim "$WORK/web" env-files 2>"$WORK/.sub.err" || true)
_sub_hit=0
_oldIFS=$IFS; IFS='
'
for _p in $_subfiles; do
  [ -n "$_p" ] || continue
  _norm=$(printf '%s\n' "$_p" | sed 's|/\./|/|g')
  if [ "$_norm" = "$WORK/web/.web.env" ] && [ -f "$_p" ]; then
    _sub_hit=1
    break
  fi
done
IFS=$_oldIFS
if [ "$_sub_hit" = 1 ]; then
  ok "web/ ./docker env-files resolves its own .web.env (self-located)"
else
  bad "web/ ./docker env-files did not resolve web/.web.env:"
  printf '%s\n' "$_subfiles" | sed 's/^/        /'
fi

if [ "$HAVE_DOCKER" -eq 1 ]; then
  _subcfg=$(run_shim "$WORK/web" compose config 2>"$WORK/.subcfg.err") || {
    bad "web/ ./docker compose config failed:"
    sed 's/^/        /' "$WORK/.subcfg.err"
    _subcfg=""
  }
  if [ -n "$_subcfg" ]; then
    _subweb=$(config_value "$_subcfg" WEB_PORT)
    if [ "$_subweb" = "$WEB_PORT_VALUE" ]; then
      ok "isolated web/ resolves WEB_PORT == $WEB_PORT_VALUE (independent of root)"
    else
      bad "isolated web/ WEB_PORT == '$_subweb' (expected $WEB_PORT_VALUE)"
    fi
  fi
else
  info "docker unavailable — skipping isolated web/ Layer-2 assertion"
fi

# --- Summary -----------------------------------------------------------------
printf '\n== summary ==\n'
printf '  passed: %s\n' "$PASS"
printf '  failed: %s\n' "$FAIL"
if [ "$FAIL" -eq 0 ]; then
  printf '\nsmoke-monorepo: PASS\n'
  exit 0
else
  printf '\nsmoke-monorepo: FAIL\n'
  exit 1
fi
