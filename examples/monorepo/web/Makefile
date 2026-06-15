# ============================================
# web subproject — standalone Makefile.
#
# Runs the web service in ISOLATION (not the unified root stack). The root
# Makefile delegates here via `make -C web <target>` (see ../Makefile).
#
# `include ../scripts/compose.mk` rides on the root's vendored scripts/ — the
# .mk resolves its script paths relative to its own location, so a subproject
# that only drops a ./docker shim (Option A) reuses the parent's engine. If this
# subproject must also build when cloned standalone, run `install.sh web/` to
# give it its OWN scripts/ copy (Option B) and switch the include to
# `include scripts/compose.mk`.
# ============================================

.DEFAULT_GOAL := help

include ../scripts/compose.mk

.PHONY: build up down config

##@ web (standalone)

build: ## Build the web image
	$(DC) build

up: ## Run web standalone (detached)
	$(DC) up -d

down: ## Stop standalone web
	$(DC) down --remove-orphans

config: ## Render web's own merged config (resolves its own WEB_PORT)
	$(DC) config
