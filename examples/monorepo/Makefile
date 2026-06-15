# ============================================
# Root Makefile — drives the UNIFIED stack and DELEGATES to subprojects.
#
# Two ways to operate the monorepo:
#   1. Unified from root — $(DC) targets act on the whole stack (root
#      docker-compose.yml include:s web/ and api/). e.g. `make up`.
#   2. Delegation — `make -C <sub> <target>` runs a subproject's OWN Makefile
#      so it builds/runs in isolation. The web-* / api-* targets below are thin
#      forwarders showing the root → subproject pattern.
#
# `include scripts/compose.mk` is what wires the toolkit: it provides
# DC / DC_PROD / PLATFORM / validate / help and (transitively) the env-debug*
# targets. Run `sh /path/to/compose-envkit/install.sh .` here first to vendor
# scripts/ + the ./docker shim.
# ============================================

.DEFAULT_GOAL := help

# Subproject delegators (defined BEFORE the include, like the root pattern).
# --no-print-directory keeps the output clean when forwarding.
SUB_WEB = $(MAKE) --no-print-directory -C web
SUB_API = $(MAKE) --no-print-directory -C api

# Wires the whole toolkit: DC/DC_PROD/PLATFORM/HOSTNAME, validate, help, and
# (transitively) the env-debug* targets. Provided by `install.sh .`.
include scripts/compose.mk

.PHONY: up down config \
        web-build web-up web-down \
        api-build api-up api-down

##@ Unified — whole stack from root

up: ## Bring the unified stack up (detached)
	$(DC) up -d

down: ## Stop the unified stack
	$(DC) down --remove-orphans

config: ## Render the merged config (proves Layer-2 across subprojects)
	$(DC) config

# validate, help, env-debug-* — provided by scripts/compose.mk

##@ Delegation — run a subproject's OWN Makefile in isolation

web-build: ## Build the web subproject standalone
	$(SUB_WEB) build
web-up: ## Run the web subproject standalone
	$(SUB_WEB) up
web-down: ## Stop the standalone web subproject
	$(SUB_WEB) down

api-build: ## Build the api subproject standalone
	$(SUB_API) build
api-up: ## Run the api subproject standalone
	$(SUB_API) up
api-down: ## Stop the standalone api subproject
	$(SUB_API) down
