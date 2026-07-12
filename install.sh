#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
PREFIX="${PREFIX:-$HOME/.local}"
FORCE=0

usage() {
    cat <<'EOF'
Usage: ./install.sh [--prefix <path>] [--force]

Installs worktree-preview, its wtp alias, and optional integration snippets.
Existing user configuration is never overwritten.
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)
            shift
            [[ $# -gt 0 ]] || { printf 'install: --prefix requires a path\n' >&2; exit 1; }
            PREFIX="$1"
            ;;
        --force)
            FORCE=1
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf 'install: unknown argument: %s\n' "$1" >&2
            usage >&2
            exit 1
            ;;
    esac
    shift
done

DESTINATION="$PREFIX/bin/worktree-preview"
ALIAS_DESTINATION="$PREFIX/bin/wtp"
mkdir -p "$PREFIX/bin"

for path in "$DESTINATION" "$ALIAS_DESTINATION"; do
    if [[ -d "$path" && ! -L "$path" ]]; then
        printf 'install: %s is a directory; refusing to replace it\n' "$path" >&2
        exit 1
    fi
    if [[ ( -e "$path" || -L "$path" ) && "$FORCE" -ne 1 ]]; then
        printf 'install: %s already exists; rerun with --force to replace it\n' "$path" >&2
        exit 1
    fi
done

if [[ "$FORCE" -eq 1 ]]; then
    for path in "$DESTINATION" "$ALIAS_DESTINATION"; do
        [[ ! -e "$path" && ! -L "$path" ]] || rm -f "$path"
    done
fi

install -m 0755 "$ROOT_DIR/bin/worktree-preview" "$DESTINATION"
ln -s worktree-preview "$ALIAS_DESTINATION"
mkdir -p "$PREFIX/share/worktree-preview/lazygit" "$PREFIX/share/worktree-preview/tmux"
install -m 0644 "$ROOT_DIR/integrations/lazygit/config.yml" \
    "$PREFIX/share/worktree-preview/lazygit/config.yml"
install -m 0644 "$ROOT_DIR/integrations/tmux/worktree-preview.conf" \
    "$PREFIX/share/worktree-preview/tmux/worktree-preview.conf"

printf 'Installed %s\n' "$DESTINATION"
printf 'Installed alias %s -> worktree-preview\n' "$ALIAS_DESTINATION"
printf 'Installed integrations under %s/share/worktree-preview\n' "$PREFIX"
printf 'Next: merge the LazyGit snippet and tmux status format into your configs.\n'
