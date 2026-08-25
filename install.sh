#!/bin/sh

set -eu

ROOT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd -P)
PREFIX=${PREFIX:-"$HOME/.local"}
FORCE=0
BUILD_BINARY=""

cleanup() {
    if [ -n "$BUILD_BINARY" ]; then
        rm -f -- "$BUILD_BINARY"
    fi
}
trap cleanup EXIT HUP INT TERM

usage() {
    cat <<'EOF'
Usage: ./install.sh [--prefix <path>] [--force]

Installs worktree-preview, its wtp alias, and the optional LazyGit snippet.
When installing from source, Go is required to build the binary.
Release archives include a prebuilt binary and do not require Go.
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix)
            shift
            if [ "$#" -eq 0 ]; then
                printf 'install: --prefix requires a path\n' >&2
                exit 1
            fi
            PREFIX=$1
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
mkdir -p -- "$PREFIX/bin"

for path in "$DESTINATION" "$ALIAS_DESTINATION"; do
    if [ -d "$path" ] && [ ! -L "$path" ]; then
        printf 'install: %s is a directory; refusing to replace it\n' "$path" >&2
        exit 1
    fi
    if { [ -e "$path" ] || [ -L "$path" ]; } && [ "$FORCE" -ne 1 ]; then
        printf 'install: %s already exists; rerun with --force to replace it\n' "$path" >&2
        exit 1
    fi
done

SOURCE_BINARY="$ROOT_DIR/bin/worktree-preview"
if [ ! -x "$SOURCE_BINARY" ]; then
    if ! command -v go >/dev/null 2>&1; then
        printf 'install: Go is required when installing from source; use a release archive for a prebuilt binary\n' >&2
        exit 1
    fi
    BUILD_BINARY=$(mktemp "${TMPDIR:-/tmp}/worktree-preview.XXXXXX")
    (cd "$ROOT_DIR" && go build -o "$BUILD_BINARY" ./cmd/worktree-preview)
    SOURCE_BINARY=$BUILD_BINARY
fi

if [ "$FORCE" -eq 1 ]; then
    for path in "$DESTINATION" "$ALIAS_DESTINATION"; do
        if [ -e "$path" ] || [ -L "$path" ]; then
            rm -f -- "$path"
        fi
    done
fi

install -m 0755 "$SOURCE_BINARY" "$DESTINATION"
ln -s worktree-preview "$ALIAS_DESTINATION"
mkdir -p -- "$PREFIX/share/worktree-preview/lazygit"
install -m 0644 "$ROOT_DIR/integrations/lazygit/config.yml" \
    "$PREFIX/share/worktree-preview/lazygit/config.yml"

printf 'Installed %s\n' "$DESTINATION"
printf 'Installed alias %s -> worktree-preview\n' "$ALIAS_DESTINATION"
printf 'Installed LazyGit snippet under %s/share/worktree-preview/lazygit\n' "$PREFIX"
