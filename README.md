# worktree-preview

[![CI](https://github.com/Octobr26/worktree-preview/actions/workflows/ci.yml/badge.svg)](https://github.com/Octobr26/worktree-preview/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey.svg)

**Review frontend changes from parallel Git worktrees through one stable localhost URL.**

When coding agents work on separate branches, reviewing their UI changes can mean switching directories, restarting dev servers, and tracking ports.
`worktree-preview` keeps that review loop on one target: select a local branch in LazyGit, press `U`, and serve its existing worktree from a dedicated tmux window at `http://localhost:3000` or your configured port.

> One branch selected. One server running. One URL to review.

```text
agent A -> worktree A --\
agent B -> worktree B ----> select in LazyGit -> tmux server -> localhost:3000
agent C -> worktree C --/
```

`worktree-preview` does not create or manage worktrees.
It switches one preview server among worktrees that already exist.

## Why

Git worktrees make it possible to work on several branches of the same repository at once.
That is especially useful when each task or coding agent has its own branch and worktree.

The remaining bottleneck is frontend review.
Without a shared preview loop, each branch can mean another directory, another server command, and another localhost port to remember.

`worktree-preview` keeps its managed preview on one configurable port and switches it to the selected worktree.

## Highlights

- Select a local branch in LazyGit and press `U` to preview its worktree.
- Keep the browser on one stable localhost URL while switching branches.
- Use `worktree-preview` as the descriptive command or `wtp` as the short alias.
- Detect pnpm, npm, Yarn, or Bun from `package.json` and supported lockfiles.
- Detect common Vite, Next.js, and Create React App scripts.
- Install dependencies when missing and refresh them when `package.json` or a supported lockfile changes.
- Refuse to interrupt a busy tmux pane or an unrelated port listener.
- Forward environment files only when explicitly configured and ignored by Git.

## Requirements

- macOS or Linux
- Bash, Git, tmux, `lsof`, and `ps`
- A local branch already checked out in a Git worktree
- An idle tmux window for the preview server
- Node.js for automatic `package.json` script detection
- pnpm, npm, Yarn, or Bun for the frontend project
- `sha256sum`, `shasum`, or OpenSSL
- LazyGit only if you want the `U` and `X` shortcuts

## Install

```sh
git clone https://github.com/Octobr26/worktree-preview.git
cd worktree-preview
./install.sh
```

The default installation creates:

```text
~/.local/bin/worktree-preview
~/.local/bin/wtp -> worktree-preview
~/.local/share/worktree-preview/
```

Add the following line to `~/.zshrc` or `~/.bashrc` if `~/.local/bin` is not already on your `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Restart the shell, or source the profile you changed, then verify both commands:

```sh
command -v worktree-preview
command -v wtp
```

Use a custom prefix when needed:

```sh
./install.sh --prefix /your/prefix
```

A custom prefix also changes the installed integration paths.
The installer refuses to replace either command unless you pass `--force`, and it never replaces a real directory.

## Set up tmux

Run the command from inside tmux.
The invoking session needs an idle window named `server` by default, ideally with one pane:

```sh
tmux new-window -n server
```

Set another window name per repository if needed:

```sh
git config worktreePreview.window preview
```

### Optional status marker

Source the installed format definition before your `status-right` setting:

```tmux
source-file ~/.local/share/worktree-preview/tmux/worktree-preview.conf
```

Add `#{E:@worktree_preview_format}` once inside the existing `status-right` value:

```tmux
set -g status-right '#{E:@worktree_preview_format} %H:%M '
```

Reload tmux:

```sh
tmux source-file ~/.tmux.conf
```

The marker shows the configured port and worktree directory, for example `3000 -> feature-checkout`.
Keep it in one `set -g status-right` definition; `set -ga` would append another segment on each reload.

## Set up LazyGit

Find the active LazyGit configuration directory:

```sh
lazygit --print-config-dir
```

Merge [`integrations/lazygit/config.yml`](integrations/lazygit/config.yml) into its `config.yml`.
The installer also copies the snippet to `~/.local/share/worktree-preview/lazygit/config.yml` with the default prefix.

If your configuration already contains `customCommands:`, add the two list entries beneath the existing key rather than adding a second `customCommands:` block.

The integration adds:

- `U` in the local-branches panel to start or switch the preview.
- `X` to stop the preview owned by `worktree-preview`.

LazyGit intentionally uses the descriptive `worktree-preview` command.
The CLI works without LazyGit.

## Use it

Create a worktree for an existing branch:

```sh
git worktree add ../my-app-feature feature/example
```

Or create a new branch and worktree together:

```sh
git worktree add -b feature/example ../my-app-feature
```

Then:

1. Open LazyGit inside the tmux session.
2. Select `feature/example` in the local-branches panel.
3. Press `U`.
4. Review the app at `http://localhost:3000`.
5. Select another worktree branch and press `U` again.
6. Refresh the same browser tab.

For daily CLI use, the short alias is equivalent to the full command:

| Command | Purpose |
| --- | --- |
| `wtp use feature/example` | Start or switch to that branch's existing worktree |
| `wtp status` | Show the recorded preview, port, PID, directory, and ownership state |
| `wtp stop` | Stop the preview process group owned by the tool |
| `wtp --dry-run feature/example` | Inspect detection without starting a server |

`worktree-preview use`, `worktree-preview status`, and the other full commands remain available.

## Automatic detection

Package-manager detection uses the `packageManager` field or a supported lockfile.
The default install commands are:

| Project | Default install |
| --- | --- |
| pnpm | `pnpm install --frozen-lockfile` |
| npm | `npm ci` |
| modern Yarn | `yarn install --immutable` |
| Bun | `bun install --frozen-lockfile` |

Start-command detection currently covers:

- Vite in a `start` or `dev` script when the script does not contain `--open`
- Next.js in a `dev` script
- Create React App in a `start` script

Override either command when the project does not match those defaults:

```sh
git config worktreePreview.install 'npm install'
git config worktreePreview.start 'npm run preview -- --port "$WORKTREE_PREVIEW_PORT"'
```

Yarn Classic projects can use:

```sh
git config worktreePreview.install 'yarn install --frozen-lockfile'
```

Projects without an npm lockfile should override the default `npm ci` command.

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
| `worktreePreview.shell` | Bash | Bash-compatible executable used for install and launch commands |

Example for a frontend subdirectory:

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
The file must exist in the main worktree and must be ignored by Git in the target worktree:

```sh
git config --add worktreePreview.envFile .env.local
```

For monorepos that install dependencies from the workspace root, keep `appDir` at `.` and use a package-targeted start command, or disable automatic installation and manage dependencies separately.

## Safety and limits

`worktree-preview` runs install and start commands from the selected worktree.
Only preview branches whose code you trust because package-manager lifecycle scripts and dev servers execute local code.

Environment files are never forwarded by default.
When explicitly configured, they are symlinked only if Git ignores the target path, but code in that worktree can still read their contents.

Before stopping a preview, the tool verifies the listener's working directory, port, and recorded process group.
It tracks the tmux pane and window as additional runtime state, never sends `Ctrl-C` into a busy or repurposed pane, and refuses to replace an unrelated listener.

Current boundaries:

- One active preview per tmux session
- Existing worktrees only
- Local branches only
- macOS and Linux
- Invocation from inside tmux
- Node-oriented automatic detection; custom commands can cover other setups

## Development

```sh
bash -n bin/worktree-preview install.sh tests/worktree-preview.sh
bash tests/worktree-preview.sh
```

CI runs the suite on macOS and Ubuntu and runs ShellCheck on Ubuntu.

## License

[MIT](LICENSE)
