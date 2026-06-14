#!/usr/bin/env zsh
# ============================================
# Tab-completion for make env-debug-* in zsh (compose-envkit).
#
# Loads the bash version through bashcompinit (a native zsh completion can be
# added later; bashcompinit covers ~99% of cases).
#
# Install:
#   source scripts/completions/env-debug.zsh
#
# Or in ~/.zshrc:
#   source /path/to/scripts/completions/env-debug.zsh
# ============================================

autoload -Uz +X bashcompinit && bashcompinit

# Find the sibling bash file (resolve this script's own directory).
_zsh_compl_dir="${0:A:h}"
if [[ -f "$_zsh_compl_dir/env-debug.bash" ]]; then
  source "$_zsh_compl_dir/env-debug.bash"
else
  echo "env-debug.zsh: env-debug.bash not found in $_zsh_compl_dir" >&2
  return 1
fi
unset _zsh_compl_dir
