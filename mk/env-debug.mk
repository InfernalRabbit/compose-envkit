# ============================================
# env-debug.mk — Reusable Makefile include for the env-chain inspector.
#
# Usually pulled in transitively by compose.mk, but can be included directly:
#   include scripts/env-debug.mk           # root project
#   include ../scripts/env-debug.mk        # vendoring subproject
#
# Provides:
#   make env-debug                — which env files load, and in what order
#   make env-debug-diff           — what EACH file adds (+) / overrides (~) / repeats (·)
#   make env-debug-effective      — final per-service values (via docker compose config)
#   make env-debug-files          — bare list of paths (for xargs/grep)
#   make env-debug-trace VAR=…    — call stack of a single variable
#   make env-debug-services       — list of services (for completion)
#   make env-debug-vars           — list of variables (for completion)
#   make install-completions      — how to enable tab-completion (bash/zsh)
#
# Variables (pass as `make <target> VAR=X SERVICE=Y`):
#   SERVICE   filter to one compose service (app | db | …)
#   VAR       filter to one variable name (required for trace)
# ============================================

# Resolve scripts/ relative to THIS include file's location.
# $(lastword $(MAKEFILE_LIST)) → path to this .mk at parse time.
# $(dir …) → its directory (with trailing slash).
# := (immediate) freezes the value so later includes don't shift it.
_ENV_DEBUG_DIR := $(dir $(lastword $(MAKEFILE_LIST)))
_ENV_DEBUG_SH  := $(_ENV_DEBUG_DIR)env-debug.sh

_ENV_DEBUG_ARGS = $(if $(SERVICE),--service $(SERVICE)) $(if $(VAR),--var $(VAR))

.PHONY: env-debug env-debug-diff env-debug-effective env-debug-files env-debug-trace \
        env-debug-services env-debug-vars install-completions

##@ Env chain inspection

env-debug: ## env chain — which files load and in what order [SERVICE=… VAR=…]
	@sh $(_ENV_DEBUG_SH) --chain $(_ENV_DEBUG_ARGS)

env-debug-diff: ## env chain — what EACH file adds (+) / overrides (~) / repeats (·) [SERVICE=… VAR=…]
	@sh $(_ENV_DEBUG_SH) --diff $(_ENV_DEBUG_ARGS)

env-debug-effective: ## env chain — final per-service values [SERVICE=… VAR=…]
	@sh $(_ENV_DEBUG_SH) --effective $(_ENV_DEBUG_ARGS)

env-debug-files: ## env chain — bare list of paths (for xargs/grep) [SERVICE=… VAR=…]
	@sh $(_ENV_DEBUG_SH) --files $(_ENV_DEBUG_ARGS)

env-debug-trace: ## env chain — call stack of one variable (requires VAR=…) [SERVICE=…]
	@test -n "$(VAR)" || { echo "Usage: make env-debug-trace VAR=NAME [SERVICE=name]"; exit 2; }
	@sh $(_ENV_DEBUG_SH) --trace $(_ENV_DEBUG_ARGS)

# Helpers for tab-completion (quiet, one value per line).
env-debug-services:
	@./docker compose config --services 2>/dev/null | sort

env-debug-vars:
	@sh $(_ENV_DEBUG_SH) --files 2>/dev/null | xargs grep -hE '^[A-Z_][A-Z0-9_]*=' 2>/dev/null | cut -d= -f1 | sort -u

install-completions: ## How to enable tab-completion for make env-debug-* (bash/zsh)
	@_COMPL="$(_ENV_DEBUG_DIR)completions/env-debug.bash"; \
	 printf '\033[1mTab-completion for make env-debug-* targets\033[0m\n\n'; \
	 printf '  Bash (one-off, current shell):\n'; \
	 printf '    \033[36msource %s\033[0m\n\n' "$$_COMPL"; \
	 printf '  Bash (persistent — add to ~/.bashrc):\n'; \
	 printf '    \033[36msource %s/%s\033[0m\n\n' "$$(pwd)" "$$_COMPL"; \
	 printf '  Zsh — add to ~/.zshrc:\n'; \
	 printf '    \033[36mautoload -Uz bashcompinit && bashcompinit\033[0m\n'; \
	 printf '    \033[36msource %s/%s\033[0m\n\n' "$$(pwd)" "$$_COMPL"; \
	 printf '  Then: \033[36mmake env-debug-trace VAR=<TAB>\033[0m → variable names,\n'; \
	 printf '        \033[36mmake env-debug SERVICE=<TAB>\033[0m → service names.\n'
