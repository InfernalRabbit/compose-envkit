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
#   6. ISOLATED api/ (Option A shim) — symmetric to 5, and assert api/ does NOT
#      see web/'s env (independent sibling chains).
#   7. Option B (self-contained) — install.sh into a standalone copy of a
#      subproject (NO reachable parent scripts/) and assert its own engine still
#      does Layer-2.
#   8. COMPOSE_ENV=prod — assert the chain still assembles and Layer-2 still
#      resolves the (flat) subproject env files (ENV-independent baseline; per-env
#      tiers/overlays land in M2).
#   9. Layer-2 OVER-discovery — a stray docker-compose*.yml NOT in the include:/
#      COMPOSE_FILE set still has its env_file: folded in (filename+depth based).
#  10. Glob limit — a compose named compose.yaml is MISSED; the SAME file renamed
#      to docker-compose.yml IS discovered (proves the glob is the cause).
#  11. COMPOSE_DEPTH boundary — a depth-4 compose is missed at the default 3 and
#      found with COMPOSE_DEPTH=4 (the depth knob works).
#  12. Host overrides — .${HOSTNAME}.env resolved + slotted before secrets, and
#      ignored under a non-matching hostname.
#  13. Both tokens — ${HOST} substitutes too (blueprint chain uses ${HOSTNAME}).
#  14. Fallback shim (no scripts/) substitutes host tokens too.
#  15. dev/prod overlay — COMPOSE_FILE ${COMPOSE_ENV} selector picks dev/prod.
#  16. Per-service env tier — web/.web.${COMPOSE_ENV}.env switches with the env.
#  17. Root per-env tier — .${ENV}.env (carrying IS_DEV) selected by COMPOSE_ENV.
#  18. Profiles — a profiled service is OFF by default, ON via COMPOSE_PROFILES.
#  19. Namespacing — a subproject env_file renames an upstream var via the chain.
#  20. Bootstrap — install generates init.sh; it seeds .X from example.X
#      (no-clobber), fans out to subproject init.sh, and is idempotent.
#  21. Deep nesting — a services/<svc>/ subproject resolves from the root at the
#      default COMPOSE_DEPTH=3 (the legacy services/ shape).
#  22. Submodule shape — a subproject with a .git gitlink is still discovered.
#
# Exit non-zero on ANY failure. The config-value checks (2/3/5/6/7/8/15/18/19)
# need `docker compose`; if it is absent those FAIL loudly — set SMOKE_SKIP_DOCKER=1
# to downgrade the docker-dependent checks to skips. The env-files discovery
# checks (4, 12, 13, 14, 16, 17, and the discovery half of 6/7/9/10/11) run with
# NO docker.
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
ISO=""   # extra temp dirs (standalone-subproject step 7 + fallback step 14 +
FBK=""   # init bootstrap step 20); all cleaned alongside $WORK
INIT=""
cleanup() { rm -rf "$WORK" ${ISO:+"$ISO"} ${FBK:+"$FBK"} ${INIT:+"$INIT"}; }
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

# Real root env from the templates (non-secret). COMPOSE_ENV stays dev.
cp "$WORK/example.env"      "$WORK/.env"
cp "$WORK/example.dev.env"  "$WORK/.dev.env"
cp "$WORK/example.prod.env" "$WORK/.prod.env"

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

# Does the env-files output ($1) contain the exact absolute path ($2)?
# Collapses any harmless `/./` segment (compose declares `env_file: ./.x.env`)
# and confirms the listed path actually exists. Returns 0 if found, 1 if not.
env_files_has() {  # env_files_has <files-output> <abs-path>
  _ef_want=$2; _ef_hit=1
  _ef_old=$IFS; IFS='
'
  for _ef_p in $1; do
    [ -n "$_ef_p" ] || continue
    _ef_norm=$(printf '%s\n' "$_ef_p" | sed 's|/\./|/|g')
    if [ "$_ef_norm" = "$_ef_want" ] && [ -f "$_ef_p" ]; then _ef_hit=0; break; fi
  done
  IFS=$_ef_old
  return $_ef_hit
}

# 1-based position of an exact path ($2) in the env-files output ($1), or empty.
# Used to assert chain ORDER (last-wins precedence). Collapses `/./`.
ef_index() {  # ef_index <files-output> <abs-path>
  printf '%s\n' "$1" | sed 's|/\./|/|g' | grep -nxF "$2" | head -1 | cut -d: -f1
}

# Extract a STRING-valued `KEY: "<value>"` from a compose-config rendering.
config_str() {  # config_str <config-text> <KEY>
  printf '%s\n' "$1" \
    | grep -E "^[[:space:]]*$2:[[:space:]]*" \
    | head -1 \
    | sed -E "s/^[[:space:]]*$2:[[:space:]]*\"?([^\"]*)\"?[[:space:]]*\$/\1/"
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
  # env_files_has collapses the harmless `/./` segment (compose declares
  # `env_file: ./.web.env`) and checks the path exists.
  if env_files_has "$_files" "$WORK/web/.web.env"; then
    ok "env-files includes web/.web.env (cross-subproject Layer-2 discovery)"
  else
    bad "env-files did NOT include web/.web.env:"
    printf '%s\n' "$_files" | sed 's/^/        /'
  fi
  if env_files_has "$_files" "$WORK/api/.api.env"; then
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
if env_files_has "$_subfiles" "$WORK/web/.web.env"; then
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

# ============================================================================
# 6. ISOLATED api/ (Option A) — api/ resolves its OWN port, blind to web/
# ============================================================================
printf '\n[6] ISOLATED api/ (Option A shim) resolves its own API_PORT\n'
cp "$WORK/docker" "$WORK/api/docker"
chmod +x "$WORK/api/docker"

_apifiles=$(run_shim "$WORK/api" env-files 2>"$WORK/.api.err" || true)
if env_files_has "$_apifiles" "$WORK/api/.api.env"; then
  ok "api/ ./docker env-files resolves its own .api.env (self-located)"
else
  bad "api/ ./docker env-files did not resolve api/.api.env:"
  printf '%s\n' "$_apifiles" | sed 's/^/        /'
fi
# Independence: an isolated api/ must NOT pull in its sibling web/'s env file.
if env_files_has "$_apifiles" "$WORK/web/.web.env"; then
  bad "isolated api/ leaked web/.web.env into its chain (should be sibling-blind)"
else
  ok "isolated api/ does not see web/.web.env (independent sibling chains)"
fi

if [ "$HAVE_DOCKER" -eq 1 ]; then
  _apicfg=$(run_shim "$WORK/api" compose config 2>"$WORK/.apicfg.err") || {
    bad "api/ ./docker compose config failed:"
    sed 's/^/        /' "$WORK/.apicfg.err"
    _apicfg=""
  }
  if [ -n "$_apicfg" ]; then
    _ap=$(config_value "$_apicfg" API_PORT)
    if [ "$_ap" = "$API_PORT_VALUE" ]; then
      ok "isolated api/ resolves API_PORT == $API_PORT_VALUE (independent of root)"
    else
      bad "isolated api/ API_PORT == '$_ap' (expected $API_PORT_VALUE)"
    fi
  fi
elif [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
  info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping isolated api/ config check"
else
  bad "docker compose unavailable — cannot check isolated api/ (set SMOKE_SKIP_DOCKER=1 to skip)"
fi

# ============================================================================
# 7. Option B — self-contained subproject (own scripts/, NO reachable parent)
# ============================================================================
printf '\n[7] Option B: standalone subproject with its OWN vendored engine\n'
ISO=$(mktemp -d 2>/dev/null || mktemp -d -t composeenvkitiso)
ISO=$(CDPATH= cd -- "$ISO" && pwd)
# Copy ONLY the web subproject's own files into a dir with no parent scripts/.
cp "$WORK/web/docker-compose.yml" "$ISO/"
cp "$WORK/web/.web.env" "$ISO/"

if sh "$KIT_DIR/install.sh" "$ISO" >"$ISO/.install.log" 2>&1; then
  ok "install.sh into a standalone subproject exited 0"
else
  bad "install.sh into a standalone subproject failed:"
  sed 's/^/        /' "$ISO/.install.log"
fi
if [ -f "$ISO/scripts/compose-env.sh" ] && [ -f "$ISO/docker" ]; then
  ok "standalone subproject has its OWN scripts/ + ./docker"
else
  bad "standalone subproject missing own scripts/ or ./docker"
fi
# Its OWN engine must still do Layer-2 (the inline shim fallback would not).
_isofiles=$(run_shim "$ISO" env-files 2>"$ISO/.ef.err" || true)
if env_files_has "$_isofiles" "$ISO/.web.env"; then
  ok "standalone subproject discovers its .web.env via its own engine (Layer-2)"
else
  bad "standalone subproject did NOT discover .web.env:"
  printf '%s\n' "$_isofiles" | sed 's/^/        /'
fi
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _isocfg=$(run_shim "$ISO" compose config 2>"$ISO/.cfg.err") || {
    bad "standalone subproject compose config failed:"
    sed 's/^/        /' "$ISO/.cfg.err"
    _isocfg=""
  }
  if [ -n "$_isocfg" ]; then
    _iw=$(config_value "$_isocfg" WEB_PORT)
    if [ "$_iw" = "$WEB_PORT_VALUE" ]; then
      ok "standalone subproject resolves WEB_PORT == $WEB_PORT_VALUE (cloneable, no parent)"
    else
      bad "standalone subproject WEB_PORT == '$_iw' (expected $WEB_PORT_VALUE)"
    fi
  fi
elif [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
  info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping standalone config check"
else
  bad "docker compose unavailable — cannot check standalone subproject (set SMOKE_SKIP_DOCKER=1 to skip)"
fi
rm -rf "$ISO"; ISO=""

# ============================================================================
# 8. COMPOSE_ENV=prod — chain still assembles; flat Layer-2 is ENV-independent
# ============================================================================
# Baseline for M1: the blueprint has no per-env tiers/overlay yet (M2 adds
# .prod.env tiers + a docker-compose.prod.yml selector). Here we only assert
# switching the env does not BREAK assembly/discovery for the flat env files.
printf '\n[8] COMPOSE_ENV=prod: chain assembles + Layer-2 still resolves\n'
_pfiles=$( export COMPOSE_ENV=prod; run_shim "$WORK" env-files 2>"$WORK/.prod.err" || true )
if env_files_has "$_pfiles" "$WORK/web/.web.env" && env_files_has "$_pfiles" "$WORK/api/.api.env"; then
  ok "COMPOSE_ENV=prod: Layer-2 still discovers web/.web.env AND api/.api.env"
else
  bad "COMPOSE_ENV=prod: Layer-2 discovery changed under prod:"
  printf '%s\n' "$_pfiles" | sed 's/^/        /'
fi
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _pcfg=$( export COMPOSE_ENV=prod; run_shim "$WORK" compose config 2>"$WORK/.prodcfg.err" ) || _pcfg=""
  if [ -n "$_pcfg" ]; then
    _pw=$(config_value "$_pcfg" WEB_PORT); _pa=$(config_value "$_pcfg" API_PORT)
    if [ "$_pw" = "$WEB_PORT_VALUE" ] && [ "$_pa" = "$API_PORT_VALUE" ]; then
      ok "COMPOSE_ENV=prod: both ports still resolve (WEB=$_pw API=$_pa)"
    else
      bad "COMPOSE_ENV=prod: ports changed (WEB=$_pw API=$_pa; expected $WEB_PORT_VALUE/$API_PORT_VALUE)"
    fi
  else
    bad "COMPOSE_ENV=prod: ./docker compose config produced no output"
  fi
elif [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
  info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping prod config check"
else
  bad "docker compose unavailable — cannot check prod config (set SMOKE_SKIP_DOCKER=1 to skip)"
fi

# ============================================================================
# 9. Layer-2 OVER-discovery — a NON-included stray compose is still folded in
# ============================================================================
# Discovery is filename + depth based, NOT include:/COMPOSE_FILE-graph aware. A
# stray docker-compose*.yml within COMPOSE_DEPTH has its env_file: discovered even
# though the root never include:s it. Documented limit (docs/monorepo.md).
printf '\n[9] OVER-discovery: env_file: from a stray (non-included) compose is folded in\n'
mkdir -p "$WORK/extra"
printf 'EXTRA_PORT=12121\n' > "$WORK/extra/.extra.env"
cat > "$WORK/extra/docker-compose-extra.yml" <<'YAML'
services:
  extra:
    image: busybox
    env_file: .extra.env
    environment:
      EXTRA_PORT: "${EXTRA_PORT:-0}"
YAML
_xfiles=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_xfiles" "$WORK/extra/.extra.env"; then
  ok "stray extra/docker-compose-extra.yml's .extra.env IS discovered (over-discovery — documented)"
else
  bad "expected over-discovery of extra/.extra.env (behavior changed — revisit docs/monorepo.md):"
  printf '%s\n' "$_xfiles" | sed 's/^/        /'
fi

# ============================================================================
# 10. Glob limit — only docker-compose*.yml is discovered (compose.yaml is missed)
# ============================================================================
printf '\n[10] GLOB limit: compose.yaml is missed; renamed docker-compose.yml is found\n'
mkdir -p "$WORK/weird"
printf 'WEIRD_PORT=14141\n' > "$WORK/weird/.weird.env"
cat > "$WORK/weird/compose.yaml" <<'YAML'
services:
  weird:
    image: busybox
    env_file: .weird.env
YAML
_wfiles=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_wfiles" "$WORK/weird/.weird.env"; then
  bad "weird/compose.yaml's .weird.env was discovered (glob unexpectedly matched compose.yaml)"
else
  ok "weird/compose.yaml is NOT discovered (only docker-compose*.yml matches — documented limit)"
fi
# Causation: the SAME file under a docker-compose*.yml name IS discovered.
mv "$WORK/weird/compose.yaml" "$WORK/weird/docker-compose.yml"
_wfiles2=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_wfiles2" "$WORK/weird/.weird.env"; then
  ok "renamed weird/docker-compose.yml IS discovered — proves the glob name was the cause"
else
  bad "renamed weird/docker-compose.yml still not discovered (cause is not the glob):"
  printf '%s\n' "$_wfiles2" | sed 's/^/        /'
fi

# ============================================================================
# 11. COMPOSE_DEPTH boundary — depth-4 missed at default 3, found at 4
# ============================================================================
printf '\n[11] COMPOSE_DEPTH boundary: depth-4 compose missed at 3, found at 4\n'
mkdir -p "$WORK/a/b/c"
printf 'DEEP_PORT=13579\n' > "$WORK/a/b/c/.deep.env"
cat > "$WORK/a/b/c/docker-compose.yml" <<'YAML'
services:
  deep:
    image: busybox
    env_file: .deep.env
YAML
# a/b/c/docker-compose.yml sits at find-depth 4 from the root → excluded at 3.
_d3=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_d3" "$WORK/a/b/c/.deep.env"; then
  bad "depth-4 .deep.env was discovered at default COMPOSE_DEPTH=3 (boundary moved):"
  printf '%s\n' "$_d3" | sed 's/^/        /'
else
  ok "depth-4 .deep.env excluded at default COMPOSE_DEPTH=3 (depth boundary holds)"
fi
_d4=$( export COMPOSE_DEPTH=4; run_shim "$WORK" env-files 2>/dev/null || true )
if env_files_has "$_d4" "$WORK/a/b/c/.deep.env"; then
  ok "depth-4 .deep.env discovered with COMPOSE_DEPTH=4 (the depth knob works)"
else
  bad "COMPOSE_DEPTH=4 did not reach a/b/c/.deep.env:"
  printf '%s\n' "$_d4" | sed 's/^/        /'
fi

# ============================================================================
# 12. Host overrides — .${HOSTNAME}.env resolved + slotted between .env & secrets
# ============================================================================
# The blueprint chain lists `.${HOSTNAME}.env` between `.${ENV}.env` and
# `.secrets.env`. With HOSTNAME pinned, the engine must substitute the token,
# discover the per-machine file, and keep secrets STRICTLY last.
printf '\n[12] Host overrides: .${HOSTNAME}.env discovered + ordered before secrets\n'
printf 'SITE_URL=host-wins.example\n' > "$WORK/.testhost.env"
printf 'API_SECRET=shhh\n'           > "$WORK/.secrets.env"
_hf=$( export HOSTNAME=testhost; run_shim "$WORK" env-files 2>/dev/null || true )
if env_files_has "$_hf" "$WORK/.testhost.env"; then
  ok "host file .testhost.env discovered via \${HOSTNAME} substitution"
else
  bad "host file .testhost.env NOT discovered (\${HOSTNAME} not substituted?):"
  printf '%s\n' "$_hf" | sed 's/^/        /'
fi
_i_env=$(ef_index "$_hf" "$WORK/.env")
_i_host=$(ef_index "$_hf" "$WORK/.testhost.env")
_i_sec=$(ef_index "$_hf" "$WORK/.secrets.env")
if [ -n "$_i_env" ] && [ -n "$_i_host" ] && [ -n "$_i_sec" ] \
   && [ "$_i_env" -lt "$_i_host" ] && [ "$_i_host" -lt "$_i_sec" ]; then
  ok "chain order .env($_i_env) < .testhost.env($_i_host) < .secrets.env($_i_sec) — secrets stay last"
else
  bad "chain order wrong (env=$_i_env host=$_i_host sec=$_i_sec; want env<host<sec):"
  printf '%s\n' "$_hf" | sed 's/^/        /'
fi
# Sanity: an UNMATCHED hostname must NOT pull the file in (proves it's keyed on host).
_hf_other=$( export HOSTNAME=otherhost; run_shim "$WORK" env-files 2>/dev/null || true )
if env_files_has "$_hf_other" "$WORK/.testhost.env"; then
  bad "host file .testhost.env discovered under a DIFFERENT hostname (not host-keyed)"
else
  ok "host file ignored under a non-matching hostname (correctly host-keyed)"
fi

# ============================================================================
# 13. Both tokens — ${HOST} substitutes too (blueprint uses ${HOSTNAME})
# ============================================================================
printf '\n[13] Both tokens: ${HOST} also substitutes, not only ${HOSTNAME}\n'
mkdir -p "$WORK/htest"
printf 'X=1\n' > "$WORK/htest/.testhost.env"
printf '%s\n' '.${HOST}.env' > "$WORK/htest/.docker-env-chain"   # literal token
cp "$WORK/docker" "$WORK/htest/docker"; chmod +x "$WORK/htest/docker"
_htf=$( export HOSTNAME=testhost; run_shim "$WORK/htest" env-files 2>/dev/null || true )
if env_files_has "$_htf" "$WORK/htest/.testhost.env"; then
  ok "\${HOST} token substitutes to the hostname (htest/.testhost.env discovered)"
else
  bad "\${HOST} token did not substitute:"
  printf '%s\n' "$_htf" | sed 's/^/        /'
fi

# ============================================================================
# 14. Fallback shim (NO engine reachable) also substitutes host tokens
# ============================================================================
# A standalone clone with just ./docker + .docker-env-chain (no scripts/) uses
# bin/docker's inline Layer-1 fallback. It must substitute host tokens too, so
# the fallback and the full engine behave consistently.
printf '\n[14] Fallback shim (no scripts/) substitutes ${HOSTNAME} in the chain\n'
FBK=$(mktemp -d 2>/dev/null || mktemp -d -t composeenvkitfbk)
FBK=$(CDPATH= cd -- "$FBK" && pwd)
cp "$KIT_DIR/bin/docker" "$FBK/docker"; chmod +x "$FBK/docker"
printf '%s\n' '.${HOSTNAME}.env' > "$FBK/.docker-env-chain"   # literal token
printf 'Z=1\n' > "$FBK/.testhost.env"
_fbf=$( export HOSTNAME=testhost; run_shim "$FBK" env-files 2>/dev/null || true )
if env_files_has "$_fbf" "$FBK/.testhost.env"; then
  ok "fallback shim substitutes \${HOSTNAME} and discovers .testhost.env (no engine)"
else
  bad "fallback shim did not substitute \${HOSTNAME}:"
  printf '%s\n' "$_fbf" | sed 's/^/        /'
fi
rm -rf "$FBK"; FBK=""

# ============================================================================
# 15. dev/prod overlay — COMPOSE_FILE ${COMPOSE_ENV} selector picks the tier
# ============================================================================
# example.env sets COMPOSE_FILE=docker-compose.yml:docker-compose.${COMPOSE_ENV}.yml.
# The plain ${COMPOSE_ENV} token resolves (shell > .env) — no re-pin needed; the
# documented footgun is only the ${VAR:+...} CONDITIONAL form. dev overlay tags
# web STACK_TIER=dev, prod overlay tags it prod.
printf '\n[15] dev/prod overlay: COMPOSE_FILE ${COMPOSE_ENV} selector\n'
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _devcfg=$(run_shim "$WORK" compose config 2>"$WORK/.o.dev.err" || true)
  _dt=$(config_str "$_devcfg" STACK_TIER)
  if [ "$_dt" = "dev" ]; then
    ok "dev: docker-compose.dev.yml overlay applied (STACK_TIER=dev)"
  else
    bad "dev: STACK_TIER='$_dt' (expected dev) — dev overlay not selected"
    sed 's/^/        /' "$WORK/.o.dev.err" 2>/dev/null || true
  fi
  _prodcfg=$( export COMPOSE_ENV=prod; run_shim "$WORK" compose config 2>"$WORK/.o.prod.err" || true )
  _pt=$(config_str "$_prodcfg" STACK_TIER)
  if [ "$_pt" = "prod" ]; then
    ok "prod: docker-compose.prod.yml overlay applied (STACK_TIER=prod)"
  else
    bad "prod: STACK_TIER='$_pt' (expected prod) — prod overlay not selected"
    sed 's/^/        /' "$WORK/.o.prod.err" 2>/dev/null || true
  fi
elif [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
  info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping overlay selection check"
else
  bad "docker compose unavailable — cannot check overlay selection (set SMOKE_SKIP_DOCKER=1 to skip)"
fi

# ============================================================================
# 16. Per-service env tier — web/.web.${COMPOSE_ENV}.env switches with the env
# ============================================================================
printf '\n[16] Per-service env tier: web/.web.${COMPOSE_ENV}.env\n'
_devf=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_devf" "$WORK/web/.web.dev.env"; then
  ok "dev: web/.web.dev.env discovered (per-service tier resolved via \${COMPOSE_ENV})"
else
  bad "dev: web/.web.dev.env NOT discovered:"
  printf '%s\n' "$_devf" | sed 's/^/        /'
fi
_prodf=$( export COMPOSE_ENV=prod; run_shim "$WORK" env-files 2>/dev/null || true )
if env_files_has "$_prodf" "$WORK/web/.web.prod.env" && ! env_files_has "$_prodf" "$WORK/web/.web.dev.env"; then
  ok "prod: web/.web.prod.env discovered AND .web.dev.env excluded (tier switched)"
else
  bad "prod: per-service tier did not switch cleanly:"
  printf '%s\n' "$_prodf" | sed 's/^/        /'
fi

# ============================================================================
# 17. Root per-env tier — .${ENV}.env (carrying IS_DEV) selected by COMPOSE_ENV
# ============================================================================
printf '\n[17] Root per-env tier .${ENV}.env (IS_DEV convention)\n'
_rdev=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_rdev" "$WORK/.dev.env" && ! env_files_has "$_rdev" "$WORK/.prod.env"; then
  ok "dev: root .dev.env selected, .prod.env excluded"
else
  bad "dev: root tier wrong (.dev.env present? / .prod.env absent?):"
  printf '%s\n' "$_rdev" | sed 's/^/        /'
fi
_rprod=$( export COMPOSE_ENV=prod; run_shim "$WORK" env-files 2>/dev/null || true )
if env_files_has "$_rprod" "$WORK/.prod.env" && ! env_files_has "$_rprod" "$WORK/.dev.env"; then
  ok "prod: root .prod.env selected, .dev.env excluded"
else
  bad "prod: root tier wrong (.prod.env present? / .dev.env absent?):"
  printf '%s\n' "$_rprod" | sed 's/^/        /'
fi
if grep -q '^IS_DEV=true$' "$WORK/.dev.env" 2>/dev/null && grep -q '^IS_DEV=false$' "$WORK/.prod.env" 2>/dev/null; then
  ok "IS_DEV convention present: .dev.env=true / .prod.env=false"
else
  bad "IS_DEV convention missing from the tier files"
fi

# ============================================================================
# 18. Profiles — a profiled service is OFF by default, ON via COMPOSE_PROFILES
# ============================================================================
# compose-envkit treats COMPOSE_PROFILES as pure passthrough: the shim execs
# `docker compose "$@"` and compose reads COMPOSE_PROFILES from the shell/.env
# natively. The blueprint ships an optional `tools` service behind profiles:[tools].
printf '\n[18] Profiles: profiled service OFF by default, ON via COMPOSE_PROFILES\n'
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _svcs=$(run_shim "$WORK" compose config --services 2>/dev/null || true)
  if printf '%s\n' "$_svcs" | grep -qx 'tools'; then
    bad "profiled 'tools' is ON by default (should be gated):"
    printf '%s\n' "$_svcs" | sed 's/^/        /'
  else
    ok "profiled 'tools' is OFF by default"
  fi
  # web/api (no profiles:) must still be active by default.
  if printf '%s\n' "$_svcs" | grep -qx 'web' && printf '%s\n' "$_svcs" | grep -qx 'api'; then
    ok "unprofiled services web + api are active by default"
  else
    bad "web/api not active by default:"; printf '%s\n' "$_svcs" | sed 's/^/        /'
  fi
  _svcs_p=$( export COMPOSE_PROFILES=tools; run_shim "$WORK" compose config --services 2>/dev/null || true )
  if printf '%s\n' "$_svcs_p" | grep -qx 'tools'; then
    ok "COMPOSE_PROFILES=tools enables 'tools' (passthrough through the shim)"
  else
    bad "COMPOSE_PROFILES=tools did NOT enable 'tools':"
    printf '%s\n' "$_svcs_p" | sed 's/^/        /'
  fi
elif [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
  info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping profiles check"
else
  bad "docker compose unavailable — cannot check profiles (set SMOKE_SKIP_DOCKER=1 to skip)"
fi

# ============================================================================
# 19. Namespacing — a subproject env_file CAN rename an upstream var via the chain
# ============================================================================
# env_file values ARE interpolated in the COMPOSE_ENV_FILES context, referencing
# vars defined EARLIER in the chain. Since the engine puts Layer 1 (root) before
# Layer 2 (subproject), a subproject can map a root var to the name its binary
# wants: NEWNAME=${ROOTVAR:-default} (the :- keeps it standalone-safe).
printf '\n[19] Namespacing: subproject env_file renames an upstream var via the chain\n'
mkdir -p "$WORK/nstest"
printf 'SITE_URL=renamed-ok\n'                  > "$WORK/nstest/.env"
printf '%s\n' '.env'                            > "$WORK/nstest/.docker-env-chain"
printf '%s\n' 'NSVC_SITE=${SITE_URL:-fallback}' > "$WORK/nstest/.nsvc.env"
cat > "$WORK/nstest/docker-compose.yml" <<'YAML'
services:
  nsvc:
    image: busybox
    env_file:
      - path: ./.nsvc.env
        required: false
    environment:
      NSVC_SITE: "${NSVC_SITE:-MISSING}"
YAML
cp "$WORK/docker" "$WORK/nstest/docker"; chmod +x "$WORK/nstest/docker"
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _nscfg=$(run_shim "$WORK/nstest" compose config 2>/dev/null || true)
  _ns=$(config_str "$_nscfg" NSVC_SITE)
  if [ "$_ns" = "renamed-ok" ]; then
    ok "subproject .nsvc.env renamed SITE_URL→NSVC_SITE via the chain (Layer 1 before Layer 2)"
  else
    bad "rename via chain failed: NSVC_SITE='$_ns' (expected renamed-ok)"
    printf '%s\n' "$_nscfg" | grep -E 'NSVC_SITE|SITE_URL' | sed 's/^/        /'
  fi
elif [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
  info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping namespacing rename check"
else
  bad "docker compose unavailable — cannot check namespacing rename (set SMOKE_SKIP_DOCKER=1 to skip)"
fi

# ============================================================================
# 20. Bootstrap init.sh — generated by install; seeds .X from example.X + fans out
# ============================================================================
# install.sh generates an executable, customizable init.sh (never clobbered). It
# seeds real env files from example.* (no-clobber), fans out to subproject
# init.sh scripts, and is idempotent. No sudo / chmod / persisted secrets.
printf '\n[20] Bootstrap init.sh: seeds .X from example.X (no-clobber), fans out\n'
INIT=$(mktemp -d 2>/dev/null || mktemp -d -t composeenvkitinit)
INIT=$(CDPATH= cd -- "$INIT" && pwd)
cp -R "$BLUEPRINT/." "$INIT/"          # example.* present; no real .env yet
if sh "$KIT_DIR/install.sh" "$INIT" >"$INIT/.install.log" 2>&1; then :; else
  bad "install into init-test dir failed:"; sed 's/^/        /' "$INIT/.install.log"
fi
if [ -f "$INIT/init.sh" ] && [ -x "$INIT/init.sh" ]; then
  ok "install.sh generated an executable init.sh"
else
  bad "init.sh not generated or not executable"
fi
# Pre-seed one target (no-clobber proof) + a subproject init.sh (fan-out proof).
printf 'COMPOSE_PROJECT_NAME=preexisting\n' > "$INIT/.env"
mkdir -p "$INIT/sub"
printf '#!/bin/sh\ntouch "$(dirname "$0")/.sub-init-ran"\n' > "$INIT/sub/init.sh"
chmod +x "$INIT/sub/init.sh"
if ( CDPATH= cd -- "$INIT" && sh ./init.sh ) >"$INIT/.init.log" 2>&1; then :; else
  bad "init.sh failed:"; sed 's/^/        /' "$INIT/.init.log"
fi
if grep -q '^COMPOSE_PROJECT_NAME=preexisting$' "$INIT/.env" 2>/dev/null; then
  ok "init.sh did NOT clobber an existing .env"
else
  bad "init.sh clobbered .env"
fi
if [ -f "$INIT/.dev.env" ] && [ -f "$INIT/.prod.env" ]; then
  ok "init.sh seeded .dev.env + .prod.env from example.*"
else
  bad "init.sh did not seed .dev.env/.prod.env"
fi
if [ -f "$INIT/sub/.sub-init-ran" ]; then
  ok "init.sh fanned out to sub/init.sh"
else
  bad "init.sh did not fan out to sub/init.sh"
fi
if ( CDPATH= cd -- "$INIT" && sh ./init.sh ) >/dev/null 2>&1; then
  ok "init.sh re-run is idempotent (exit 0)"
else
  bad "init.sh re-run failed (not idempotent)"
fi
rm -rf "$INIT"; INIT=""

# ============================================================================
# 21. Deep nesting — a services/<svc>/ subproject resolves from the root (depth 3)
# ============================================================================
# The legacy monorepo nests services as services/<name>/. The blueprint ships one
# (services/reports/) included by the root; cross-subproject Layer-2 reaches its
# compose file at find-depth 3 (the default COMPOSE_DEPTH).
printf '\n[21] Deep nesting: services/<svc>/ resolves from the root at depth 3\n'
_deepf=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_deepf" "$WORK/services/reports/.reports.env"; then
  ok "Layer-2 discovers services/reports/.reports.env from root (default COMPOSE_DEPTH=3)"
else
  bad "services/reports/.reports.env NOT discovered from root:"
  printf '%s\n' "$_deepf" | sed 's/^/        /'
fi
if [ "$HAVE_DOCKER" -eq 1 ]; then
  _deepcfg=$(run_shim "$WORK" compose config 2>/dev/null || true)
  _rp=$(config_value "$_deepcfg" REPORTS_PORT)
  if [ "$_rp" = "15151" ]; then
    ok "cross-subproject Layer-2 resolves REPORTS_PORT=15151 for nested services/reports"
  else
    bad "REPORTS_PORT='$_rp' (expected 15151) — deep subproject not resolved"
    printf '%s\n' "$_deepcfg" | grep -E 'REPORTS_PORT|published' | sed 's/^/        /'
  fi
elif [ "${SMOKE_SKIP_DOCKER:-0}" = "1" ]; then
  info "docker unavailable + SMOKE_SKIP_DOCKER=1 — skipping deep config check"
else
  bad "docker compose unavailable — cannot check deep subproject (set SMOKE_SKIP_DOCKER=1 to skip)"
fi

# ============================================================================
# 22. Submodule shape — a subproject with a .git gitlink is still discovered
# ============================================================================
# A subproject can be a git submodule (its own repo). To the kit it is just a
# subdir: discovery is find-by-glob, blind to .git. Prove a .git gitlink file
# doesn't change anything.
printf '\n[22] Submodule shape: a subproject with a .git gitlink is discovered\n'
mkdir -p "$WORK/vendored"
printf 'gitdir: /elsewhere/.git/modules/vendored\n' > "$WORK/vendored/.git"  # submodule gitlink
printf 'VEND_PORT=16161\n' > "$WORK/vendored/.vend.env"
cat > "$WORK/vendored/docker-compose.yml" <<'YAML'
services:
  vend:
    image: busybox
    env_file: .vend.env
YAML
_subm=$(run_shim "$WORK" env-files 2>/dev/null || true)
if env_files_has "$_subm" "$WORK/vendored/.vend.env"; then
  ok "subproject with a .git gitlink is discovered like any subdir (submodules just work)"
else
  bad "submodule-shaped subproject not discovered:"
  printf '%s\n' "$_subm" | sed 's/^/        /'
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
