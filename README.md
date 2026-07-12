# worktree-preview

Switch one frontend preview server between existing Git worktrees from LazyGit or the command line.

`worktree-preview` runs the selected branch in a dedicated tmux window, keeps the preview on one port, and refuses to interrupt panes or listeners it does not own.
It was extracted from a personal dotfiles workflow and packaged here as a standalone tool for macOS and Linux.

## What it does

- Maps a local branch to its existing Git worktree.
- Detects pnpm, npm, Yarn, or Bun projects.
- Detects common Vite, Next.js, and Create React App start commands.
- Installs dependencies when missing and refreshes them when package metadata changes.
- Reuses one configurable port and one dedicated tmux window.
- Adds optional LazyGit shortcuts and a tmux status marker.
- Tracks the preview process so unrelated panes and port owners are left alone.

## Requirements

- macOS or Linux
- Bash, Git, tmux, `lsof`, and `ps`
- Node.js for `package.json` script detection
- pnpm, npm, Yarn, or Bun for the project being previewed
- `sha256sum`, `shasum`, or OpenSSL
- LazyGit only if you want the `U` and `X` shortcuts

## Install

```sh
git clone https://github.com/Octobr26/worktree-preview.git
cd worktree-preview
./install.sh
```

The default destination is `~/.local/bin/worktree-preview`.
Make sure `~/.local/bin` is on your `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Use `./install.sh --prefix /your/prefix` to choose another prefix.
The installer refuses to replace an existing executable unless you pass `--force`.

## Set up tmux

Each project session needs a dedicated window named `server` by default:

```sh
tmux new-window -n server
```

Source the optional preview format before your `status-right` definition:

```tmux
source-file ~/.local/share/worktree-preview/tmux/worktree-preview.conf
```

Add `#{E:@worktree_preview_format}` once inside your existing `status-right` value.
For example:

```tmux
set -g status-right '#{E:@worktree_preview_format} %H:%M '
```

Keep this in one `set -g status-right` definition.
Using `set -ga` for the segment would append another copy on every config reload.

Reload tmux after changing the config:

```sh
tmux source-file ~/.tmux.conf
```

Set a different window name per repository if `server` is already used:

```sh
git config worktreePreview.window preview
```

## Set up LazyGit

Merge [`integrations/lazygit/config.yml`](integrations/lazygit/config.yml) into your LazyGit `config.yml`.
The installed copy is at `~/.local/share/worktree-preview/lazygit/config.yml` with the default prefix.

The integration adds these commands:

- `U` in the local-branches panel starts or switches the preview.
- `X` stops the preview owned by `worktree-preview`.

The command-line interface works without LazyGit.

## Use it

Create a worktree for each branch you want to preview:

```sh
git worktree add ../my-app-feature feature/example
```

Open LazyGit inside the tmux session, select `feature/example` in local branches, and press `U`.
The tool installs that worktree's dependencies if needed, starts its dev server in the dedicated window, and records the active preview in tmux.
Press `U` on another branch to switch the same port to its worktree.

You can also run it directly from inside tmux:

```sh
worktree-preview use feature/example
worktree-preview status
worktree-preview stop
```

Inspect detection without starting tmux or changing anything:

```sh
worktree-preview --dry-run feature/example
```

## Repository configuration

Configuration is stored in the repository's local Git config.

| Key | Default | Purpose |
| --- | --- | --- |
| `worktreePreview.appDir` | `.` | App directory relative to each worktree |
| `worktreePreview.port` | `3000` | Preview port |
| `worktreePreview.packageManager` | detected | `pnpm`, `npm`, `yarn`, or `bun` |
| `worktreePreview.install` | detected | Dependency install command; use `none` to disable |
| `worktreePreview.start` | detected | Dev-server command |
| `worktreePreview.envFile` | none | Ignored environment file to link from the main worktree; repeatable |
| `worktreePreview.window` | `server` | Dedicated tmux window name |
| `worktreePreview.shell` | Bash | Bash-compatible shell used for install and launch commands |

Example for a frontend subdirectory and a custom Vite command:

```sh
git config worktreePreview.appDir frontend
git config worktreePreview.port 4173
git config worktreePreview.start 'pnpm run dev -- --port "$WORKTREE_PREVIEW_PORT" --strictPort'
```

Disable automatic dependency installation:

```sh
git config worktreePreview.install none
```

Environment forwarding is opt-in.
The file must already exist in the main worktree and must be ignored by Git in the target worktree:

```sh
git config --add worktreePreview.envFile .env.local
```

For monorepos with dependencies installed from the workspace root, keep `appDir` at `.` and use a package-targeted start command, or disable automatic installation and manage dependencies separately.

## Safety boundary

`worktree-preview` runs install and start commands from the selected worktree.
Only preview branches whose code you trust because package-manager lifecycle scripts and dev servers execute local code.

Environment files are never forwarded by default.
When explicitly configured, they are symlinked only if Git ignores the target path, but code in that worktree can still read their contents.

The tool records the pane, process group, working directory, port, and window for previews it starts.
It signals only a process group it can verify against the recorded runtime state and will not send `Ctrl-C` into a repurposed pane.

## Tests

```sh
bash -n bin/worktree-preview install.sh tests/worktree-preview.sh
bash tests/worktree-preview.sh
```

CI runs the test suite on macOS and Ubuntu and runs ShellCheck on Ubuntu.

## License

[MIT](LICENSE)
