#!/usr/bin/env bash

set -u
set -o pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
CLI="$ROOT_DIR/bin/worktree-preview"
ORIGINAL_PATH="$PATH"
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/worktree-preview-tests.XXXXXX")
TMP_ROOT=$(cd "$TMP_ROOT" && pwd -P)
PASS_COUNT=0
FAIL_COUNT=0

cleanup() {
    rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

assert_contains() {
    local haystack="$1"
    local needle="$2"

    case "$haystack" in
        *"$needle"*) return 0 ;;
        *) printf 'expected output to contain: %s\nactual output:\n%s\n' "$needle" "$haystack" >&2; return 1 ;;
    esac
}

assert_file_contains() {
    local file="$1"
    local needle="$2"

    [[ -f "$file" ]] || { printf 'expected file: %s\n' "$file" >&2; return 1; }
    grep -Fq -- "$needle" "$file" || {
        printf 'expected %s to contain: %s\n' "$file" "$needle" >&2
        return 1
    }
}

create_fixture() {
    CASE_DIR=$(mktemp -d "$TMP_ROOT/case.XXXXXX")
    CASE_DIR=$(cd "$CASE_DIR" && pwd -P)
    MAIN_REPO="$CASE_DIR/main repo"
    TARGET_WORKTREE="$CASE_DIR/feature worktree"
    TEST_HOME="$CASE_DIR/home"
    TEST_STATE="$CASE_DIR/state"

    mkdir -p "$MAIN_REPO" "$TEST_HOME" "$TEST_STATE"
    git init -q -b main "$MAIN_REPO"
    git -C "$MAIN_REPO" config user.name "Test User"
    git -C "$MAIN_REPO" config user.email "test@example.com"

    cat > "$MAIN_REPO/package.json" <<'EOF'
{
  "packageManager": "pnpm@10.0.0",
  "scripts": {
    "dev": "vite"
  }
}
EOF
    printf 'lockfileVersion: 9\n' > "$MAIN_REPO/pnpm-lock.yaml"
    printf '.env\n.env.local\nnode_modules/\n' > "$MAIN_REPO/.gitignore"
    git -C "$MAIN_REPO" add package.json pnpm-lock.yaml .gitignore
    git -C "$MAIN_REPO" commit -qm "fixture"
    git -C "$MAIN_REPO" worktree add -q -b feature/demo "$TARGET_WORKTREE"
    printf 'TEST_VALUE=1\n' > "$MAIN_REPO/.env"

    export HOME="$TEST_HOME"
    export XDG_STATE_HOME="$TEST_STATE"
    export GIT_CONFIG_GLOBAL=/dev/null
    export GIT_CONFIG_NOSYSTEM=1
    export SHELL=/bin/bash
    export PATH="$ORIGINAL_PATH"
}

prepare_fakes() {
    FAKE_BIN="$CASE_DIR/fake-bin"
    FAKE_TMUX_STATE="$CASE_DIR/tmux-state"
    FAKE_LISTENER_STATE="$CASE_DIR/listener"
    FAKE_TMUX_LOG="$CASE_DIR/tmux.log"
    FAKE_PACKAGE_LOG="$CASE_DIR/package.log"
    FAKE_KILL_LOG="$CASE_DIR/kill.log"
    FAKE_PANE_COMMAND_STATE="$CASE_DIR/pane-command"
    FAKE_FOREGROUND_PGID_STATE="$CASE_DIR/foreground-pgid"
    FAKE_TARGET_APP="$TARGET_WORKTREE"
    FAKE_PANE_COMMAND="${FAKE_PANE_COMMAND:-bash}"
    FAKE_SHELL_PID=1111

    mkdir -p "$FAKE_BIN" "$FAKE_TMUX_STATE/options"
    : > "$FAKE_TMUX_LOG"
    : > "$FAKE_PACKAGE_LOG"
    : > "$FAKE_KILL_LOG"
    printf '%s\n' "$FAKE_PANE_COMMAND" > "$FAKE_PANE_COMMAND_STATE"
    printf '%s\n' "$FAKE_SHELL_PID" > "$FAKE_FOREGROUND_PGID_STATE"

    cat > "$FAKE_BIN/tmux" <<'EOF'
#!/usr/bin/env bash
set -u

last=""
previous=""
for argument in "$@"; do
    previous="$last"
    last="$argument"
done

printf 'tmux' >> "$FAKE_TMUX_LOG"
for argument in "$@"; do
    printf ' %q' "$argument" >> "$FAKE_TMUX_LOG"
done
printf '\n' >> "$FAKE_TMUX_LOG"

option_file() {
    printf '%s/options/%s' "$FAKE_TMUX_STATE" "${1#@}"
}

case "${1:-}" in
    display-message)
        case "$last" in
            '#{session_name}') printf 'preview-session\n' ;;
            '#{pane_current_command}') cat "$FAKE_PANE_COMMAND_STATE" ;;
            '#{pane_pid}') printf '%s\n' "$FAKE_SHELL_PID" ;;
        esac
        ;;
    list-panes)
        printf '%%7\n'
        ;;
    show-option)
        file=$(option_file "$last")
        [[ -f "$file" ]] && cat "$file"
        ;;
    set-option)
        unset_option=0
        for argument in "$@"; do
            [[ "$argument" == "-u" ]] && unset_option=1
        done
        if [[ "$unset_option" -eq 1 ]]; then
            rm -f "$(option_file "$last")"
        else
            printf '%s\n' "$last" > "$(option_file "$previous")"
        fi
        ;;
    send-keys)
        if [[ "$last" == "C-c" ]]; then
            printf 'tmux C-c\n' >> "$FAKE_KILL_LOG"
        elif [[ "$last" == "C-m" ]]; then
            printf '4242|%s|4242|3000\n' "$FAKE_TARGET_APP" > "$FAKE_LISTENER_STATE"
            printf '4242\n' > "$FAKE_FOREGROUND_PGID_STATE"
            printf 'node\n' > "$FAKE_PANE_COMMAND_STATE"
        fi
        ;;
    capture-pane)
        printf 'fake server output\n'
        ;;
esac
EOF

    cat > "$FAKE_BIN/lsof" <<'EOF'
#!/usr/bin/env bash
set -u

[[ -f "$FAKE_LISTENER_STATE" ]] || exit 1
IFS='|' read -r listener_pid listener_cwd listener_pgid listener_port < "$FAKE_LISTENER_STATE"

case "$*" in
    *-tiTCP:*)
        requested_port=""
        for argument in "$@"; do
            case "$argument" in
                -tiTCP:*) requested_port=${argument#-tiTCP:} ;;
            esac
        done
        [[ "$requested_port" == "$listener_port" ]] && printf '%s\n' "$listener_pid"
        ;;
    *'-d cwd'*)
        requested_pid=""
        previous=""
        for argument in "$@"; do
            if [[ "$previous" == "-p" ]]; then
                requested_pid="$argument"
            fi
            previous="$argument"
        done
        if [[ "$requested_pid" == "$listener_pid" ]]; then
            printf 'p%s\nfcwd\nn%s\n' "$listener_pid" "$listener_cwd"
        fi
        ;;
esac
EOF

    cat > "$FAKE_BIN/ps" <<'EOF'
#!/usr/bin/env bash
set -u

field=""
requested_pid=""
previous=""
for argument in "$@"; do
    if [[ "$previous" == "-o" ]]; then
        field="$argument"
    elif [[ "$previous" == "-p" ]]; then
        requested_pid="$argument"
    fi
    previous="$argument"
done

if [[ "$field" == "tpgid=" && "$requested_pid" == "$FAKE_SHELL_PID" ]]; then
    cat "$FAKE_FOREGROUND_PGID_STATE"
    exit 0
fi

if [[ "$field" == "pgid=" && "$requested_pid" == "$FAKE_SHELL_PID" ]]; then
    printf ' %s\n' "$FAKE_SHELL_PID"
    exit 0
fi

[[ -f "$FAKE_LISTENER_STATE" ]] || exit 1
IFS='|' read -r listener_pid listener_cwd listener_pgid listener_port < "$FAKE_LISTENER_STATE"
if [[ "$field" == "pgid=" && "$requested_pid" == "$listener_pid" ]]; then
    printf ' %s\n' "$listener_pgid"
fi
EOF

    cat > "$FAKE_BIN/kill" <<'EOF'
#!/usr/bin/env bash
set -u

printf 'kill' >> "$FAKE_KILL_LOG"
for argument in "$@"; do
    printf ' %s' "$argument" >> "$FAKE_KILL_LOG"
done
printf '\n' >> "$FAKE_KILL_LOG"

[[ -f "$FAKE_LISTENER_STATE" ]] || exit 1
IFS='|' read -r listener_pid listener_cwd listener_pgid listener_port < "$FAKE_LISTENER_STATE"
last=""
for argument in "$@"; do
    last="$argument"
done
requested_pgid=${last#-}
[[ "$requested_pgid" == "$listener_pgid" ]] || exit 1
rm -f "$FAKE_LISTENER_STATE"
if [[ "$(<"$FAKE_PANE_COMMAND_STATE")" == "node" ]]; then
    printf 'bash\n' > "$FAKE_PANE_COMMAND_STATE"
    printf '%s\n' "$FAKE_SHELL_PID" > "$FAKE_FOREGROUND_PGID_STATE"
fi
EOF

    cat > "$FAKE_BIN/pnpm" <<'EOF'
#!/usr/bin/env bash
set -u
printf '%s|%s\n' "$PWD" "$*" >> "$FAKE_PACKAGE_LOG"
case "${1:-}" in
    install) mkdir -p "$PWD/node_modules" ;;
esac
EOF

    cat > "$FAKE_BIN/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF

    chmod 755 "$FAKE_BIN/tmux" "$FAKE_BIN/lsof" "$FAKE_BIN/ps" "$FAKE_BIN/kill"
    chmod 755 "$FAKE_BIN/pnpm" "$FAKE_BIN/sleep"
    export FAKE_BIN FAKE_TMUX_STATE FAKE_LISTENER_STATE FAKE_TMUX_LOG FAKE_PACKAGE_LOG FAKE_KILL_LOG
    export FAKE_PANE_COMMAND_STATE FAKE_FOREGROUND_PGID_STATE FAKE_SHELL_PID
    export FAKE_TARGET_APP FAKE_PANE_COMMAND
    export WORKTREE_PREVIEW_KILL_COMMAND="$FAKE_BIN/kill"
    export PATH="$FAKE_BIN:$ORIGINAL_PATH"
}

test_dry_run_resolves_worktree() {
    local output

    create_fixture
    output=$(cd "$MAIN_REPO" && "$CLI" --dry-run refs/heads/feature/demo)
    assert_contains "$output" "branch: feature/demo" || return 1
    assert_contains "$output" "worktree: $TARGET_WORKTREE" || return 1
    assert_contains "$output" "package manager: pnpm" || return 1
    assert_contains "$output" "start: pnpm run dev --port \"\$WORKTREE_PREVIEW_PORT\" --strictPort" || return 1
    assert_contains "$output" "environment files: none" || return 1
}

test_config_overrides() {
    local output

    create_fixture
    git -C "$MAIN_REPO" config worktreePreview.port 4173
    git -C "$MAIN_REPO" config worktreePreview.packageManager npm
    git -C "$MAIN_REPO" config worktreePreview.install none
    git -C "$MAIN_REPO" config worktreePreview.start "npm run preview"
    git -C "$MAIN_REPO" config --add worktreePreview.envFile .env

    output=$(cd "$MAIN_REPO" && "$CLI" --dry-run feature/demo)
    assert_contains "$output" "port: 4173" || return 1
    assert_contains "$output" "package manager: npm" || return 1
    assert_contains "$output" "install: none" || return 1
    assert_contains "$output" "start: npm run preview" || return 1
    assert_contains "$output" "environment files: .env" || return 1
}

test_use_reuse_and_stop() {
    local output
    local first_send_count
    local second_send_count
    local expected_shell

    create_fixture
    prepare_fakes
    git -C "$MAIN_REPO" config worktreePreview.install "$FAKE_BIN/pnpm install --frozen-lockfile"
    git -C "$MAIN_REPO" config --add worktreePreview.envFile .env

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo)
    assert_contains "$output" "Linked .env" || return 1
    assert_contains "$output" "Serving feature worktree" || return 1
    [[ -L "$TARGET_WORKTREE/.env" ]] || { printf 'expected environment symlink\n' >&2; return 1; }
    [[ "$(readlink "$TARGET_WORKTREE/.env")" == "$MAIN_REPO/.env" ]] || return 1
    assert_file_contains "$FAKE_TMUX_STATE/options/worktree_preview_status" "3000 -> feature worktree" || return 1
    assert_file_contains "$FAKE_TMUX_STATE/options/worktree_preview_pgid" "4242" || return 1
    assert_file_contains "$FAKE_TMUX_STATE/options/worktree_preview_port" "3000" || return 1
    assert_file_contains "$FAKE_TMUX_STATE/options/worktree_preview_window" "server" || return 1
    assert_file_contains "$FAKE_TMUX_STATE/options/worktree_preview_shell_pid" "$FAKE_SHELL_PID" || return 1
    assert_file_contains "$FAKE_PACKAGE_LOG" "$TARGET_WORKTREE|install --frozen-lockfile" || return 1
    assert_file_contains "$FAKE_TMUX_LOG" "WORKTREE_PREVIEW_BRANCH=feature/demo" || return 1
    expected_shell=$(command -v bash)
    assert_file_contains "$FAKE_TMUX_LOG" "$expected_shell" || return 1
    assert_file_contains "$FAKE_TMUX_LOG" "-lc" || return 1

    first_send_count=$(grep -c 'send-keys' "$FAKE_TMUX_LOG")
    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo)
    second_send_count=$(grep -c 'send-keys' "$FAKE_TMUX_LOG")
    assert_contains "$output" "Already serving feature worktree" || return 1
    [[ "$first_send_count" -eq "$second_send_count" ]] || return 1

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" stop)
    assert_contains "$output" "Stopped preview-session:server" || return 1
    [[ ! -e "$FAKE_LISTENER_STATE" ]] || return 1
    [[ ! -e "$FAKE_TMUX_STATE/options/worktree_preview_status" ]] || return 1
}

test_repurposed_pane_is_not_interrupted_on_stop() {
    local output

    create_fixture
    prepare_fakes
    git -C "$MAIN_REPO" config worktreePreview.install none
    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo)
    assert_contains "$output" "Serving feature worktree" || return 1
    printf 'nvim\n' > "$FAKE_PANE_COMMAND_STATE"

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" stop)
    assert_contains "$output" "Stopped preview-session:server" || return 1
    [[ "$(<"$FAKE_PANE_COMMAND_STATE")" == "nvim" ]] || return 1
    [[ ! -e "$FAKE_LISTENER_STATE" ]] || return 1
    assert_file_contains "$FAKE_KILL_LOG" "kill -INT -- -4242" || return 1
    if grep -Fq "tmux C-c" "$FAKE_KILL_LOG"; then
        printf 'stop sent Ctrl-C into a repurposed pane\n' >&2
        return 1
    fi
}

test_listener_pid_replacement_keeps_ownership() {
    local output

    create_fixture
    prepare_fakes
    git -C "$MAIN_REPO" config worktreePreview.install none
    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo)
    assert_contains "$output" "Serving feature worktree" || return 1
    printf '4343|%s|4242|3000\n' "$TARGET_WORKTREE" > "$FAKE_LISTENER_STATE"

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" status)
    assert_contains "$output" "pid: 4343" || return 1
    assert_contains "$output" "state: owned" || return 1
    assert_file_contains "$FAKE_TMUX_STATE/options/worktree_preview_pid" "4343" || return 1

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" stop)
    assert_contains "$output" "Stopped preview-session:server" || return 1
    [[ ! -e "$FAKE_LISTENER_STATE" ]] || return 1
}

test_foreground_listener_group_refreshes_ownership() {
    local output

    create_fixture
    prepare_fakes
    git -C "$MAIN_REPO" config worktreePreview.install none
    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo)
    assert_contains "$output" "Serving feature worktree" || return 1
    printf '4343|%s|4343|3000\n' "$TARGET_WORKTREE" > "$FAKE_LISTENER_STATE"
    printf '4343\n' > "$FAKE_FOREGROUND_PGID_STATE"

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" status)
    assert_contains "$output" "pid: 4343" || return 1
    assert_contains "$output" "state: owned" || return 1
    assert_file_contains "$FAKE_TMUX_STATE/options/worktree_preview_pgid" "4343" || return 1

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" stop)
    assert_contains "$output" "Stopped preview-session:server" || return 1
    [[ ! -e "$FAKE_LISTENER_STATE" ]] || return 1
}

test_unverified_listener_replacement_is_preserved() {
    local output

    create_fixture
    prepare_fakes
    git -C "$MAIN_REPO" config worktreePreview.install none
    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo)
    assert_contains "$output" "Serving feature worktree" || return 1
    printf '4343|%s|4343|3000\n' "$TARGET_WORKTREE" > "$FAKE_LISTENER_STATE"
    printf '7777\n' > "$FAKE_FOREGROUND_PGID_STATE"
    printf 'nvim\n' > "$FAKE_PANE_COMMAND_STATE"

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" status)
    assert_contains "$output" "state: stale" || return 1
    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" stop)
    assert_contains "$output" "Cleared stale preview state" || return 1
    [[ -e "$FAKE_LISTENER_STATE" ]] || return 1
    [[ ! -s "$FAKE_KILL_LOG" ]] || return 1
}

test_stop_uses_stored_port_and_window() {
    local output

    create_fixture
    prepare_fakes
    git -C "$MAIN_REPO" config worktreePreview.install none
    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo)
    assert_contains "$output" "Serving feature worktree" || return 1
    git -C "$MAIN_REPO" config worktreePreview.port 4173
    git -C "$MAIN_REPO" config worktreePreview.window preview

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" status)
    assert_contains "$output" "server window: preview-session:server" || return 1
    assert_contains "$output" "port: 3000" || return 1

    output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" stop)
    assert_contains "$output" "Stopped preview-session:server" || return 1
    [[ ! -e "$FAKE_LISTENER_STATE" ]] || return 1
}

test_foreign_port_is_not_interrupted() {
    local output
    local status

    create_fixture
    prepare_fakes
    printf '9999|/tmp/unrelated|9999|3000\n' > "$FAKE_LISTENER_STATE"

    if output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "port 3000 belongs to unowned PID 9999 (/tmp/unrelated)" || return 1
    [[ "$(<"$FAKE_LISTENER_STATE")" == "9999|/tmp/unrelated|9999|3000" ]] || return 1
}

test_busy_pane_is_not_interrupted() {
    local output
    local status

    create_fixture
    FAKE_PANE_COMMAND=nvim
    prepare_fakes

    if output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "tmux pane %7 is busy with nvim" || return 1
}

test_environment_file_must_be_ignored() {
    local output
    local status

    create_fixture
    prepare_fakes
    printf 'SECRET=example\n' > "$MAIN_REPO/secrets.env"
    git -C "$MAIN_REPO" config --add worktreePreview.envFile secrets.env

    if output=$(cd "$MAIN_REPO" && TMUX_PANE='%1' "$CLI" use feature/demo 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "refusing to link environment file that Git does not ignore: secrets.env" || return 1
    [[ ! -e "$TARGET_WORKTREE/secrets.env" ]] || return 1
}

test_invalid_port_is_rejected() {
    local output
    local status

    create_fixture
    git -C "$MAIN_REPO" config worktreePreview.port 70000

    if output=$(cd "$MAIN_REPO" && "$CLI" --dry-run feature/demo 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "preview port out of range: 70000" || return 1
}

test_installer_refuses_unexpected_overwrite() {
    local alias_collision_prefix
    local directory_prefix
    local output
    local prefix
    local status

    create_fixture
    prefix="$CASE_DIR/prefix with spaces"
    output=$(PREFIX="$prefix" "$ROOT_DIR/install.sh")
    assert_contains "$output" "Installed $prefix/bin/worktree-preview" || return 1
    assert_contains "$output" "Installed alias $prefix/bin/wtp -> worktree-preview" || return 1
    [[ -x "$prefix/bin/worktree-preview" ]] || return 1
    [[ -L "$prefix/bin/wtp" && -x "$prefix/bin/wtp" ]] || return 1
    [[ "$(readlink "$prefix/bin/wtp")" == "worktree-preview" ]] || return 1
    output=$("$prefix/bin/wtp" --help)
    assert_contains "$output" "Short alias after installation: wtp" || return 1

    if output=$(PREFIX="$prefix" "$ROOT_DIR/install.sh" 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "already exists; rerun with --force" || return 1
    PREFIX="$prefix" "$ROOT_DIR/install.sh" --force >/dev/null
    [[ -L "$prefix/bin/wtp" ]] || return 1

    alias_collision_prefix="$CASE_DIR/alias collision"
    mkdir -p "$alias_collision_prefix/bin"
    printf 'keep me\n' > "$alias_collision_prefix/bin/wtp"
    if output=$(PREFIX="$alias_collision_prefix" "$ROOT_DIR/install.sh" 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "$alias_collision_prefix/bin/wtp already exists" || return 1
    [[ ! -e "$alias_collision_prefix/bin/worktree-preview" ]] || return 1
    [[ "$(<"$alias_collision_prefix/bin/wtp")" == "keep me" ]] || return 1
    PREFIX="$alias_collision_prefix" "$ROOT_DIR/install.sh" --force >/dev/null
    [[ -L "$alias_collision_prefix/bin/wtp" ]] || return 1

    directory_prefix="$CASE_DIR/directory collision"
    mkdir -p "$directory_prefix/bin/wtp"
    if output=$(PREFIX="$directory_prefix" "$ROOT_DIR/install.sh" --force 2>&1); then
        status=0
    else
        status=$?
    fi
    [[ "$status" -ne 0 ]] || return 1
    assert_contains "$output" "$directory_prefix/bin/wtp is a directory" || return 1
    [[ -d "$directory_prefix/bin/wtp" ]] || return 1
    [[ ! -e "$directory_prefix/bin/worktree-preview" ]] || return 1
}

test_tmux_integration_is_idempotent() {
    local config="$ROOT_DIR/integrations/tmux/worktree-preview.conf"
    local format
    local socket
    local status_right

    assert_file_contains "$config" "set -g @worktree_preview_format" || return 1
    if grep -Eq 'set[[:space:]]+-g?a[^[:space:]]*[[:space:]]+status-right' "$config"; then
        printf 'tmux integration appends status-right on every reload\n' >&2
        return 1
    fi

    command -v tmux >/dev/null 2>&1 || return 0
    socket="worktree-preview-test-$$-$RANDOM"
    tmux -L "$socket" -f /dev/null new-session -d 2>/dev/null || return 0
    tmux -L "$socket" set-option -g status-right BASE 2>/dev/null || {
        tmux -L "$socket" kill-server 2>/dev/null || true
        return 0
    }
    tmux -L "$socket" source-file "$config" || {
        tmux -L "$socket" kill-server 2>/dev/null || true
        return 1
    }
    tmux -L "$socket" source-file "$config" || {
        tmux -L "$socket" kill-server 2>/dev/null || true
        return 1
    }
    format=$(tmux -L "$socket" show-option -gv @worktree_preview_format)
    status_right=$(tmux -L "$socket" show-option -gv status-right)
    tmux -L "$socket" kill-server

    assert_contains "$format" "@worktree_preview_status" || return 1
    [[ "$status_right" == "BASE" ]] || return 1
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

run_test "dry run resolves branch worktree" test_dry_run_resolves_worktree
run_test "repo-local config overrides defaults" test_config_overrides
run_test "use, reuse, and stop own preview safely" test_use_reuse_and_stop
run_test "repurposed pane is not interrupted on stop" test_repurposed_pane_is_not_interrupted_on_stop
run_test "listener PID replacement keeps ownership" test_listener_pid_replacement_keeps_ownership
run_test "foreground listener group refreshes ownership" test_foreground_listener_group_refreshes_ownership
run_test "unverified listener replacement is preserved" test_unverified_listener_replacement_is_preserved
run_test "stop uses stored port and window" test_stop_uses_stored_port_and_window
run_test "foreign port owner is preserved" test_foreign_port_is_not_interrupted
run_test "busy tmux pane is preserved" test_busy_pane_is_not_interrupted
run_test "environment link must be ignored" test_environment_file_must_be_ignored
run_test "invalid port is rejected" test_invalid_port_is_rejected
run_test "installer refuses unexpected overwrite" test_installer_refuses_unexpected_overwrite
run_test "tmux integration is idempotent" test_tmux_integration_is_idempotent

printf '%s passed, %s failed\n' "$PASS_COUNT" "$FAIL_COUNT"
[[ "$FAIL_COUNT" -eq 0 ]]
