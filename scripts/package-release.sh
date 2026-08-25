#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
    printf 'usage: scripts/package-release.sh <version>\n' >&2
    exit 1
fi

ROOT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")/.." && pwd -P)
VERSION=${1#v}
DIST_DIR="$ROOT_DIR/dist"

case "$VERSION" in
    ''|*[!A-Za-z0-9._-]*)
        printf 'package-release: invalid version: %s\n' "$VERSION" >&2
        exit 1
        ;;
esac

if [ -e "$DIST_DIR" ]; then
    printf 'package-release: %s already exists; remove it before packaging\n' "$DIST_DIR" >&2
    exit 1
fi
mkdir "$DIST_DIR"

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
    goos=${target%/*}
    goarch=${target#*/}
    package_name="worktree-preview_${VERSION}_${goos}_${goarch}"
    package_dir="$DIST_DIR/$package_name"
    mkdir -p "$package_dir/bin" "$package_dir/integrations/lazygit"

    (
        cd "$ROOT_DIR"
        CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
            go build -trimpath -ldflags='-s -w' -o "$package_dir/bin/worktree-preview" ./cmd/worktree-preview
    )
    cp "$ROOT_DIR/install.sh" "$ROOT_DIR/README.md" "$ROOT_DIR/LICENSE" "$package_dir/"
    cp "$ROOT_DIR/integrations/lazygit/config.yml" "$package_dir/integrations/lazygit/"
    tar -C "$package_dir" -czf "$DIST_DIR/$package_name.tar.gz" .
done

if command -v sha256sum >/dev/null 2>&1; then
    (cd "$DIST_DIR" && sha256sum ./*.tar.gz > checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
    (cd "$DIST_DIR" && shasum -a 256 ./*.tar.gz > checksums.txt)
else
    printf 'package-release: no SHA-256 command found\n' >&2
    exit 1
fi

printf 'Created release archives in %s\n' "$DIST_DIR"
