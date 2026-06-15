# ============================================
# api subproject — standalone Makefile.
#
# Runs the api service in ISOLATION (not the unified root stack). The root
# Makefile delegates here via `make -C api <target>` (see ../Makefile).
#
# `include ../scripts/compose.mk` rides on the root's vendored scripts/ — the
# .mk resolves its script paths relative to its own location, so a subproject
# that only drops a ./docker shim (Option A) reuses the parent's engine. If this
# subproject must also build when cloned standalone, run `install.sh api/` to
# give it its OWN scripts/ copy (Option B) and switch the include to
# `include scripts/compose.mk`.
# ============================================

.DEFAULT_GOAL := help

include ../scripts/compose.mk

.PHONY: build up down config

##@ api (standalone)

build: ## Build the api image
	$(DC) build

up: ## Run api standalone (detached)
	$(DC) up -d

down: ## Stop standalone api
	$(DC) down --remove-orphans

config: ## Render api's own merged config (resolves its own API_PORT)
	$(DC) config
