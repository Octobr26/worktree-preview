# worktree-preview

[![CI](https://github.com/Octobr26/worktree-preview/actions/workflows/ci.yml/badge.svg)](https://github.com/Octobr26/worktree-preview/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey.svg)

**Review frontend changes from Git worktrees through stable localhost URLs.**

`worktree-preview` starts development servers in the background, records which repository owns each port, and switches a configured port between existing worktrees.
It does not require tmux and it never creates or manages worktrees.

```text
worktree A --\
worktree B ----> select in LazyGit -> background preview -> localhost:3000
worktree C --/

another repository ---------------------------> localhost:4173
```

Use one stable port for a repository whose browser origin must not change, or configure different ports for repositories that should run at the same time.
`wtp list` shows every preview managed by the tool and the repository, branch, and worktree associated with each port.

## Highlights

- Start or switch a preview from LazyGit with `P`.
- Stop the current repository's preview with `X`.
- Keep each repository on a stable configured port.
- Run previews as detached process groups without a terminal multiplexer.
- List all managed preview ports with `wtp list`.
- Keep logs under the user's state directory and inspect them with `wtp logs`.
- Detect pnpm, npm, Yarn, or Bun without invoking Node to parse `package.json`.
- Support explicit commands, a `worktree-preview` package script, and limited Vite, Next.js, and Create React App adapters.
- Refuse to replace a port owned by another repository or an unowned listener.
- Forward environment files only when explicitly configured and ignored by Git.

## Requirements

- macOS or Linux
- Git
- An existing local branch checked out in a Git worktree
- The package manager and runtime required by the target project
- LazyGit only for the optional `P` and `X` shortcuts

The released CLI is a standalone binary.
Go is only required when building or installing directly from source.

## Install

Download the archive for your operating system and architecture from [GitHub Releases](https://github.com/Octobr26/worktree-preview/releases), extract it, and run:

```sh
./install.sh
```

Release archives include a prebuilt binary, so the installation does not require Go.

To build from a source checkout instead:

```sh
git clone https://github.com/Octobr26/worktree-preview.git
cd worktree-preview
./install.sh
```

Source installation requires Go.

The default installation creates:

```text
~/.local/bin/worktree-preview
~/.local/bin/wtp -> worktree-preview
~/.local/share/worktree-preview/lazygit/config.yml
```

Add `~/.local/bin` to your `PATH` when needed:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Use a custom prefix or explicitly replace an earlier installation:

```sh
./install.sh --prefix /your/prefix
./install.sh --force
```

## Set up LazyGit

Find the active LazyGit configuration directory:

```sh
lazygit --print-config-dir
```

Merge [`integrations/lazygit/config.yml`](integrations/lazygit/config.yml) into that directory's `config.yml`.
If the file already contains `customCommands:`, add the two entries beneath the existing key.

The integration adds:

- `P` in the worktrees panel to start or switch the selected worktree's preview.
- `X` anywhere in LazyGit to stop the preview owned by the current repository on its configured port.

## Use it

Create a worktree for an existing branch:

```sh
git worktree add ../my-app-feature feature/example
```

Then select that worktree in LazyGit and press `P`, or run:

```sh
wtp use feature/example
```

The command waits until the exact configured port is ready and then returns while the server continues in the background.

| Command | Purpose |
| --- | --- |
| `wtp use feature/example` | Point the configured port at an existing branch worktree, replacing any preview this repository is running |
| `wtp stop` | Stop the preview owned by the current repository on its configured port |
| `wtp status` | Show the current repository's preview and ownership state |
| `wtp list` | Show all managed ports and their repositories, branches, and worktrees |
| `wtp logs` | Show the latest 80 log lines for the current repository's preview |
| `wtp dry-run feature/example` | Show the resolved target and commands without changing runtime state |

`worktree-preview` and the short `wtp` alias are equivalent.

### Switching worktrees

A repository previews one worktree at a time on one stable port.
Running `use` against another worktree of the same repository switches the port over to it:

```sh
wtp use feature/other
```

```
Stopping feature/example on port 3000
Serving my-app-other
url: http://localhost:3000
```

The browser URL never changes, so a reload shows the other worktree.
There is no flag to run two worktrees of one repository at the same time.

A preview owned by a *different* repository is never stopped, and neither is an unowned listener on the port.
Both fail with the owner named.

## Start-command resolution

The repository owns how its application starts.
`worktree-preview` owns the stable-port contract by exporting `WORKTREE_PREVIEW_PORT` and `PORT`, adding known framework flags, waiting for the exact port, and cleaning up failed launches.

The resolver uses this order:

1. The repository-local `worktreePreview.start` Git configuration.
2. A `package.json` script named `worktree-preview`.
3. A limited adapter for a recognized Vite, Next.js, or Create React App script.
4. A safe failure with instructions to configure an explicit command.

It does not run an arbitrary `start` or `dev` script when the framework cannot be recognized.

An adapter applies only when the framework is really installed.
The package manager answers that question, not the `package.json` dependency maps.
This recognizes frameworks that a template or a workspace hoists without declaring them.
When the package manager cannot answer, the resolver reads the installed `node_modules` tree, then the declared dependencies.

Detection therefore runs after dependency installation.
A `dry-run` on a worktree with no dependencies reports `start: unresolved`, which is expected.

Example explicit command:

```sh
git config worktreePreview.start 'pnpm run preview -- --port "$WORKTREE_PREVIEW_PORT"'
```

The command must remain in the foreground and listen on the configured port.
The CLI detaches and supervises the command's process group itself.

The package manager itself is detected from the `packageManager` field or a supported lockfile.
Default dependency commands are:

| Package manager | Default install |
| --- | --- |
| pnpm | `pnpm install --frozen-lockfile` |
| npm | `npm ci` |
| modern Yarn | `yarn install --immutable` |
| Bun | `bun install --frozen-lockfile` |

Disable automatic dependency installation when the repository manages it another way:

```sh
git config worktreePreview.install none
```

## Repository configuration

Configuration is stored in the repository's local Git config and is shared by its worktrees.

| Key | Default | Purpose |
| --- | --- | --- |
| `worktreePreview.appDir` | `.` | App directory relative to each worktree |
| `worktreePreview.port` | `3000` | Stable preview port for this repository |
| `worktreePreview.packageManager` | detected | `pnpm`, `npm`, `yarn`, or `bun` |
| `worktreePreview.install` | detected | Dependency command; use `none` to disable |
| `worktreePreview.start` | detected | Explicit foreground dev-server command |
| `worktreePreview.envFile` | none | Ignored environment file to link from the main worktree; repeatable |
| `worktreePreview.shell` | `/bin/sh` | Shell used for install and start commands |

Example for a frontend subdirectory on a second port:

```sh
git config worktreePreview.appDir frontend
git config worktreePreview.port 4173
git config worktreePreview.start 'pnpm run dev -- --port "$WORKTREE_PREVIEW_PORT" --strictPort'
```

Environment forwarding remains opt-in.
The source must exist in the main worktree and the destination must be ignored by Git:

```sh
git config --add worktreePreview.envFile .env.local
```

## Runtime state and safety

State is stored under `${XDG_STATE_HOME:-$HOME/.local/state}/worktree-preview`.
There is one state record and operation lock per managed port, plus a log file and dependency receipts.

Each preview records:

- Repository identity through Git's resolved common directory
- Main repository and target worktree paths
- Branch and app directory
- Port
- Launcher PID and process-group ID
- Process start fingerprint
- Command, start time, and log path

Before signaling a process group, the CLI verifies the repository identity, PID, process group, and process start fingerprint.
It refuses to stop another repository's preview, refuses to replace an unowned listener, and fails closed when recorded ownership can no longer be verified.

The tool runs package-manager lifecycle scripts and development servers from selected worktrees.
Only preview code you trust.

Current boundaries:

- One managed preview per port
- One worktree previewed per repository at a time; `use` switches between them
- Multiple repositories may run simultaneously on different ports
- Existing local worktrees only
- macOS and Linux
- Custom start commands must stay in the foreground

## Development

```sh
go build ./cmd/worktree-preview
go vet ./...
go test ./...
bash tests/install.sh
```

Release packaging cross-compiles standalone macOS and Linux binaries for AMD64 and ARM64.

## License

[MIT](LICENSE)
