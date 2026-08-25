#!/usr/bin/env bash

# Checks that install.sh places the binary and the wtp alias, and that it never
# replaces an existing file unless --force is given.

set -u
set -o pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/worktree-preview-install-tests.XXXXXX")
TMP_ROOT=$(cd "$TMP_ROOT" && pwd -P)
PASS_COUNT=0
FAIL_COUNT=0
CASE_DIR=""

cleanup() {
    rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

assert_contains() {
    local haystack="$1"
    local needle="$2"

    case "$haystack" in
        *"$needle"*) return 0 ;;
        *)
            printf 'expected output to contain: %s\nactual output:\n%s\n' "$needle" "$haystack" >&2
            return 1
            ;;
    esac
}

new_case() {
    CASE_DIR=$(mktemp -d "$TMP_ROOT/case.XXXXXX")
}

test_installs_binary_and_alias() {
    local output
    local prefix

    new_case
    prefix="$CASE_DIR/prefix with spaces"
    output=$(PREFIX="$prefix" "$ROOT_DIR/install.sh") || return 1
    assert_contains "$output" "Installed $prefix/bin/worktree-preview" || return 1
    assert_contains "$output" "Installed alias $prefix/bin/wtp -> worktree-preview" || return 1
    [[ -x "$prefix/bin/worktree-preview" ]] || return 1
    [[ -L "$prefix/bin/wtp" && -x "$prefix/bin/wtp" ]] || return 1
    [[ "$(readlink "$prefix/bin/wtp")" == "worktree-preview" ]] || return 1

    output=$("$prefix/bin/wtp" --help) || return 1
    assert_contains "$output" "Short alias after installation: wtp" || return 1
    assert_contains "$output" "previews one worktree at a time" || return 1
}

test_refuses_overwrite_without_force() {
    local output
    local prefix
    local status

    new_case
    prefix="$CASE_DIR/prefix"
    PREFIX="$prefix" "$ROOT_DIR/install.sh" >/dev/null || return 1

    if output=$(PREFIX="$prefix" "$ROOT_DIR/install.sh" 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "already exists; rerun with --force" || return 1

    PREFIX="$prefix" "$ROOT_DIR/install.sh" --force >/dev/null || return 1
    [[ -L "$prefix/bin/wtp" ]] || return 1
}

test_preserves_unrelated_alias_file() {
    local output
    local prefix
    local status

    new_case
    prefix="$CASE_DIR/alias collision"
    mkdir -p "$prefix/bin"
    printf 'keep me\n' > "$prefix/bin/wtp"

    if output=$(PREFIX="$prefix" "$ROOT_DIR/install.sh" 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "$prefix/bin/wtp already exists" || return 1
    [[ ! -e "$prefix/bin/worktree-preview" ]] || return 1
    [[ "$(<"$prefix/bin/wtp")" == "keep me" ]] || return 1
}

test_refuses_to_replace_a_directory() {
    local output
    local prefix
    local status

    new_case
    prefix="$CASE_DIR/directory collision"
    mkdir -p "$prefix/bin/wtp"

    if output=$(PREFIX="$prefix" "$ROOT_DIR/install.sh" --force 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "$prefix/bin/wtp is a directory" || return 1
    [[ -d "$prefix/bin/wtp" ]] || return 1
    [[ ! -e "$prefix/bin/worktree-preview" ]] || return 1
}

run_test() {
    local name="$1"
    shift

    if ( "$@" ); then
        printf 'ok - %s\n' "$name"
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        printf 'not ok - %s\n' "$name" >&2
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
}

run_test "installs the binary and the wtp alias" test_installs_binary_and_alias
run_test "refuses to overwrite without --force" test_refuses_overwrite_without_force
run_test "preserves an unrelated wtp file" test_preserves_unrelated_alias_file
run_test "refuses to replace a directory" test_refuses_to_replace_a_directory

printf '%s passed, %s failed\n' "$PASS_COUNT" "$FAIL_COUNT"
[[ "$FAIL_COUNT" -eq 0 ]]
