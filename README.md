<div align="center">

# 🗂️&nbsp;&nbsp;`csm`

### Copilot Session Manager

***Browse, search, resume, and clean up your GitHub Copilot CLI sessions from a terminal UI.***

<p>
  <a href="https://github.com/gfarb/copilot-session-manager/releases/latest"><img src="https://img.shields.io/github/v/release/gfarb/copilot-session-manager?style=for-the-badge&color=212&logo=github" alt="latest release"></a>
  <img src="https://img.shields.io/badge/platform-macOS-000?style=for-the-badge&logo=apple" alt="macOS">
  <img src="https://img.shields.io/badge/built%20with-Go-00ADD8?style=for-the-badge&logo=go" alt="Go">
  <a href="https://github.com/charmbracelet/bubbletea"><img src="https://img.shields.io/badge/powered%20by-Bubble%20Tea-FF75B7?style=for-the-badge" alt="Bubble Tea"></a>
</p>

<br>

![csm demo](docs/demo.gif)

</div>

<br>

> [!TIP]
> `csm` → fuzzy-search your sessions → <kbd>enter</kbd> to resume → <kbd>o</kbd> to open files → <kbd>d</kbd> to delete.

---

## Why this exists

Copilot CLI stores every session locally on disk: a SQLite database plus per-session folders with event logs, generated artifacts, checkpoints, and plan files.

There's no built-in way to browse any of it. `csm` gives you:

| | |
|---|---|
| 🔍 **Find** | Fuzzy-search across summaries, repos, branches, file paths, refs, and messages |
| 📂 **Browse** | Files the assistant touched, parsed from `events.jsonl` |
| 🎨 **Open** | Artifacts, plans, and checkpoints directly from the TUI |
| ⏪ **Resume** | One keystroke, correct cwd, clean handoff to `copilot --resume` |
| 🗑️ **Delete** | Hard-delete with safety checks (refuses to delete active sessions) |
| 🚀 **Integrate** | `/session-manager` slash command launches `csm` from inside Copilot CLI |

---

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/gfarb/copilot-session-manager/main/scripts/install.sh | bash
```

The installer downloads the latest release binary and the `/session-manager` slash command extension:

| What | Where |
|---|---|
| `csm` binary | `~/.local/bin/csm` (override with `INSTALL_DIR`) |
| Slash command | `~/.copilot/extensions/csm/extension.mjs` |

> [!IMPORTANT]
> Make sure `~/.local/bin` is on your `PATH`. The installer verifies the download against a published `sha256` sidecar.

<details>
<summary><strong>Build from source</strong></summary>

Requires **Go 1.26+**.

```sh
git clone https://github.com/gfarb/copilot-session-manager
cd copilot-session-manager
go build -o ~/.local/bin/csm .

# (optional) slash command
mkdir -p ~/.copilot/extensions/csm
cp extension/extension.mjs ~/.copilot/extensions/csm/extension.mjs
```

</details>

Verify it works:

```sh
csm -h
csm list | head -3
```

---

## Keyboard Shortcuts

Two-pane layout: session list on the left, tabbed preview on the right.

#### Navigation

| Key | Action |
|---|---|
| <kbd>↑</kbd> <kbd>↓</kbd> / <kbd>j</kbd> <kbd>k</kbd> | Move through the list |
| <kbd>/</kbd> | Fuzzy filter (searches summary, repo, branch, cwd, file paths, refs, and user messages) |
| <kbd>enter</kbd> | **Resume** the session (`chdir` + `copilot --resume`) |
| <kbd>d</kbd> | **Delete** the session (confirm with <kbd>y</kbd>) |
| <kbd>c</kbd> / <kbd>C</kbd> | Copy session **id** / **cwd** to clipboard |
| <kbd>1</kbd> <kbd>2</kbd> <kbd>3</kbd> <kbd>4</kbd> | Jump to tab |
| <kbd>[</kbd> / <kbd>]</kbd> | Cycle tabs |
| <kbd>tab</kbd> | Swap focus between list and preview |
| <kbd>?</kbd> | Toggle key hints |
| <kbd>q</kbd> / <kbd>ctrl-c</kbd> | Quit |

#### Files tab

Files found in session metadata and tool events:

| Badge | Meaning |
|---|---|
| `[plan]` | Session plan (`plan.md`) |
| `[artifact]` | Generated files in `files/` |
| `[checkpoint]` | Checkpoint files |
| `[view]` `[edit]` `[create]` | Workspace files parsed from `events.jsonl` |

Paths are displayed relative to the session's cwd when possible:

```text
./pkg/quota/limiter.go         <- inside the session's cwd
~/other-repo/foo.go            <- outside cwd, inside $HOME
/etc/hosts                     <- absolute path
files/dashboard.json           <- session artifact
```

| Key | Action |
|---|---|
| <kbd>o</kbd> | Open with macOS default app |
| <kbd>O</kbd> | Reveal in Finder |
| <kbd>v</kbd> | Open in VS Code |
| <kbd>e</kbd> | Open in `$EDITOR` (TUI suspends, resumes on exit) |
| <kbd>y</kbd> | Copy full path |

#### Refs tab

PRs, issues, commits, and other GitHub URLs mentioned in the session, parsed from `events.jsonl`.

| Key | Action |
|---|---|
| <kbd>o</kbd> | Open URL in browser |
| <kbd>y</kbd> | Copy URL |

#### Overview & Turns

- **Overview** (<kbd>1</kbd>): IDs, dates, summary, first/last user message
- **Turns** (<kbd>4</kbd>): Scrollable conversation history with timestamps

---

## `/session-manager` Slash Command

Type this inside any live Copilot CLI session:

```
/session-manager
```

`csm` opens in a new terminal window (Ghostty, iTerm, or Terminal.app, detected via `TERM_PROGRAM`). Your active session keeps running undisturbed.

Pass args through:

```
/session-manager --print
```

---

## CLI Subcommands

```sh
csm                  # interactive TUI (default)
csm --print          # TUI, but enter prints "<cwd>\t<id>" to stdout
csm list             # TSV dump: id, display-line, search-blob
csm preview <id>     # human-readable session detail
csm -h               # help
```

`--print` is useful for shell scripts:

```sh
read -r CWD ID < <(csm --print)
echo "picked $ID in $CWD"
```

---

## Configuration

All optional. Everything works out of the box.

| Env var | Default | What it does |
|---|---|---|
| `CSM_DB` | `~/.copilot/session-store.db` | Session database path |
| `CSM_SESSION_STATE` | `<CSM_DB-dir>/session-state` | Session state directory |
| `CSM_BIN` | `csm` | Binary the slash command launches |
| `EDITOR` | `vi` | Editor for the <kbd>e</kbd> action |
| `INSTALL_DIR` | `~/.local/bin` | Where the installer puts the binary |
| `CSM_VERSION` | `latest` | Pin a specific release tag |

---

## How it works

- **SQLite** - reads from `~/.copilot/session-store.db` via [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) (pure Go, no CGo). A read-only connection handles queries; a separate writable connection is opened only for deletes.
- **Events parser** - scans `events.jsonl` line by line so multi-megabyte payloads don't silently truncate.
- **Resume** - on <kbd>enter</kbd>, the TUI exits, `os.Chdir`s to the session's cwd, then `syscall.Exec`s `copilot --resume=<id>`. The process is replaced in one move. `chdir` failure is fatal to prevent resuming in the wrong directory.
- **Delete** - a single transaction across all session tables, then `os.RemoveAll` on the session-state folder. Refuses if `inuse.*.lock` is present. Tolerates schema drift across Copilot CLI versions.

---

## Development

```sh
go build -o csm . && go test ./...
./csm
```

Iterating on the extension:

```sh
# Edit extension/extension.mjs, then:
cp extension/extension.mjs ~/.copilot/extensions/csm/extension.mjs
# In your live Copilot CLI session, /clear forces extension reload.
```

Cutting a release: push a `v*` tag. The workflow at `.github/workflows/release.yml` cross-compiles darwin arm64/amd64 and attaches the archives to a GitHub Release.

```sh
git tag v0.1.0
git push origin v0.1.0
```
