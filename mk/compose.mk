# ============================================
# compose.mk — neutral Makefile glue for docker-compose + compose-envkit.
#
# Include (one line wires the whole toolkit):
#   include scripts/compose.mk          # root project
#   include ../scripts/compose.mk       # subproject that vendors its own kit
#
# What it provides:
#   - Variables: DC, DC_PROD, PLATFORM, DBP, HOSTNAME (override BEFORE include)
#   - Targets:   validate (compose config check), help (grouped by ##@)
#   - Transitive include of env-debug.mk → env-debug* targets + completions
#
# What it does NOT provide (keep these per-project in YOUR Makefile):
#   - info / dev / prod / deploy / build / package and any service names,
#     hostnames, or overlay lists. This file stays project-agnostic.
#
# Vendor contract: the lib scripts (compose-env.sh, parse-compose-env-files.sh,
# env-debug.sh) and these .mk files live together under <target>/scripts/.
# Paths to the scripts are resolved relative to THIS file (see env-debug.mk),
# so the include works from the repo root and from a vendoring subproject alike.
# ============================================

# --- Variables (override in your Makefile BEFORE the include) ---

# Target build platform for `docker buildx` / image pulls. Empty = host default.
PLATFORM ?= linux/amd64

# The compose entrypoint. `./docker` is the compose-envkit shim that assembles
# COMPOSE_ENV_FILES (project chain + discovered service env_file: paths) before
# delegating to `docker compose`. Override DC to point elsewhere if needed.
DC       ?= ./docker compose

# Production invocation. Neutral base: select the prod env layer via COMPOSE_ENV,
# which makes the chain resolve `.prod.env` and lets `${COMPOSE_ENV:-dev}` in your
# COMPOSE_FILE pick the prod overlay. NO project-specific overlay is hardcoded here.
#
# To extend with project overlays, override DC_PROD in YOUR Makefile, e.g.:
#   # force a TLS/ingress overlay on every prod call (belt-and-suspenders):
#   DC_PROD = COMPOSE_ENV=prod COMPOSE_TLS=true ./docker compose
# or drive it purely through COMPOSE_FILE in .prod.env:
#   # .prod.env: COMPOSE_FILE=docker-compose.yml:docker-compose.tls.yml:docker-compose.prod.yml
#   DC_PROD = COMPOSE_ENV=prod ./docker compose
DC_PROD  ?= COMPOSE_ENV=prod ./docker compose

# Inject DOCKER_DEFAULT_PLATFORM only when PLATFORM is non-empty.
DBP       = $(if $(PLATFORM),DOCKER_DEFAULT_PLATFORM=$(PLATFORM),)

# HOSTNAME may be unset in some shells/CI; provide a deterministic fallback so
# compose interpolation of ${HOSTNAME} never yields a blank.
ifeq ($(origin HOSTNAME), undefined)
  export HOSTNAME := $(shell hostname 2>/dev/null || echo unknown)
endif

# --- Path resolution for transitively-included scripts ---
# $(lastword $(MAKEFILE_LIST)) is THIS file at parse time; $(dir …) is its
# directory (with trailing slash). := freezes it so later includes don't shift it.
_COMPOSE_MK_DIR := $(dir $(lastword $(MAKEFILE_LIST)))

# --- Targets ---

.PHONY: validate help

##@ Common

validate: ## Validate compose configs (dev + prod)
	@$(DC) config -q && echo "  dev:  OK"
	@$(DC_PROD) config -q && echo "  prod: OK"

help: ## Show available commands (grouped by ##@ sections)
	@awk 'BEGIN {FS = ":.*?## "} \
	     /^##@ / { printf "\n  \033[1;33m%s\033[0m\n", substr($$0, 5); next } \
	     /^[a-zA-Z_][a-zA-Z0-9_-]*:.*?##/ { printf "    \033[36m%-22s\033[0m %s\n", $$1, $$2 }' \
	     $(MAKEFILE_LIST)
	@printf "\n  \033[2mTab-completion: make install-completions\033[0m\n\n"

# --- Transitive include: env-debug.mk gives the env-chain inspection targets ---

include $(_COMPOSE_MK_DIR)env-debug.mk
