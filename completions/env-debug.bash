#!/usr/bin/env bash
# ============================================
# Tab-completion for the make env-debug-* targets (compose-envkit).
#
# Install:
#   source scripts/completions/env-debug.bash          # current shell
#   echo "source $PWD/scripts/completions/env-debug.bash" >> ~/.bashrc
#
# What it does:
#   make env-deb<TAB>                — list env-debug-* targets
#   make env-debug-trace VAR=<TAB>   — variable names from the env chain
#   make env-debug SERVICE=<TAB>     — service names from docker compose
#   make env-debug-trace VAR=DB_<TAB>— prefix filtering
#
# Zsh loads this file via bashcompinit (see env-debug.zsh / make install-completions).
#
# NOTE: this is a bash completion script, so bash-isms below are intentional —
# it is NOT a POSIX /bin/sh script and is never run under sh.
# ============================================

_env_debug_targets() {
  cat <<EOF
env-debug
env-debug-diff
env-debug-effective
env-debug-files
env-debug-trace
env-debug-services
env-debug-vars
install-completions
EOF
}

_env_debug_var_names() {
  # Stay silent if make/scripts are unavailable (another project, broken setup).
  make -s env-debug-vars 2>/dev/null
}

_env_debug_service_names() {
  make -s env-debug-services 2>/dev/null
}

_env_debug_complete() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  # 1) VAR=… or SERVICE=… — complete with values
  case "$cur" in
    VAR=*)
      local pattern="${cur#VAR=}"
      local vars
      vars=$(_env_debug_var_names)
      COMPREPLY=( $(compgen -W "$vars" -P "VAR=" -- "$pattern") )
      return 0
      ;;
    SERVICE=*)
      local pattern="${cur#SERVICE=}"
      local services
      services=$(_env_debug_service_names)
      COMPREPLY=( $(compgen -W "$services" -P "SERVICE=" -- "$pattern") )
      return 0
      ;;
  esac

  # 2) After env-debug-trace with no VAR= yet → suggest VAR=
  if [[ "$prev" == env-debug-trace ]]; then
    COMPREPLY=( $(compgen -W "VAR=" -- "$cur") )
    return 0
  fi

  # 3) After any env-debug-* — suggest VAR= and SERVICE=
  if [[ "$prev" == env-debug* ]]; then
    COMPREPLY=( $(compgen -W "VAR= SERVICE=" -- "$cur") )
    return 0
  fi

  # 4) Complete target names (when the previous word is `make`)
  if [[ "$prev" == make ]]; then
    # Our env-debug-* targets + whatever `make help` exposes (dedup via sort -u)
    local all_targets
    all_targets=$(
      {
        _env_debug_targets
        make -s help 2>/dev/null | awk '/^[[:space:]]*\033\[36m/ { gsub(/\033\[[0-9;]*m/, ""); print $1 }'
      } | sort -u
    )
    COMPREPLY=( $(compgen -W "$all_targets" -- "$cur") )
    return 0
  fi

  # 5) Fallback: defer to standard make completion if present
  if declare -F _make >/dev/null 2>&1; then
    _make
    return $?
  fi

  return 0
}

# Register for `make`. This overrides any previously-installed `complete -F`
# for make, which is what we want; remove the line to restore the default.
complete -F _env_debug_complete make
