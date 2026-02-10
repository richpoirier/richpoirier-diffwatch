# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Install

```bash
go build ./...          # compile check
make install            # build and install to ~/bin/diffwatch
```

There are no tests yet. No linter is configured.

## What This Is

diffwatch is a terminal UI (bubbletea) that watches one or more git repos for uncommitted changes, showing a file tree on the left and a syntax-highlighted diff (via `delta`) on the right. It polls `git status` every second rather than using filesystem watchers.

## Architecture

- **main.go** — CLI entry point. Parses args, handles profile flags (`--save`, `--list`, `--delete`), resolves paths/profiles, discovers repos, starts watcher and TUI.
- **git.go** — Git operations and repo discovery. `DiscoverRepos` finds repos by walking down or up from a given path. `GetChangedFiles` runs `git status --porcelain`. `GetDiff` pipes `git diff` through `delta`. Core types: `Repo` (with `Path` for git root and `WatchPath` for scoped subtree) and `ChangedFile`.
- **model.go** — Root bubbletea model. Owns layout (split panels), dispatches messages to filetree and diffview sub-models. Handles `FilesChangedMsg` and `FileSelectedMsg` routing.
- **filetree.go** — Left panel. Flat list of `RepoGroup`s (collapsible) with files underneath. Cursor navigation auto-loads diffs. Supports `/` filter mode. Has ANSI-aware truncation for long paths.
- **diffview.go** — Right panel. Wraps a `viewport` for scrollable diff content. Supports hunk navigation (`n`/`N`).
- **watcher.go** — Polls `git status` every second per repo. Uses fingerprinting to only emit `FilesChangedMsg` when state actually changes.
- **config.go** — Profile system. Stores named path lists in `~/.config/diffwatch/config.json`. Handles `--save`, `--list`, `--delete`, and profile resolution.

## Key Design Decisions

- **`WatchPath` vs `Path`**: A `Repo` has both a git root (`Path`) and a scoped subtree (`WatchPath`). Multiple entries can share the same `Path` (e.g., two subdirs of a monorepo). **Always use `WatchPath` as the unique identity key**, never `Path` — using `Path` causes flickering/collision bugs.
- **Polling over fsnotify**: fsnotify was removed because it opens an fd per watched directory, which crashes large repos with "pipe failed". Polling `git status` is simpler and has no fd limits.
- **Worktree support**: `isGitRepo` checks for `.git` as either a directory or a file (worktree pointer). `findGitRoot` walks up the directory tree to find the repo root when given a subdirectory.

## Runtime Dependency

Requires `delta` (git-delta) on PATH for syntax-highlighted diffs.
