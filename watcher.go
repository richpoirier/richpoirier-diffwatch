package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// FilesChangedMsg is sent when a repo's changed files have been refreshed.
type FilesChangedMsg struct {
	Repo  *Repo
	Files []ChangedFile
}

// Watcher watches git repos for file changes and sends debounced updates.
type Watcher struct {
	fsw   *fsnotify.Watcher
	repos []Repo
	msgCh chan FilesChangedMsg
	done  chan struct{}

	mu      sync.Mutex
	pending map[string]bool // repo paths with pending changes
	timer   *time.Timer
}

// NewWatcher creates a Watcher for the given repos, registers all directories
// for watching, and starts the event loop.
func NewWatcher(repos []Repo) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fsw:     fsw,
		repos:   repos,
		pending: make(map[string]bool),
		msgCh:   make(chan FilesChangedMsg, 64),
		done:    make(chan struct{}),
	}

	for _, repo := range repos {
		if err := w.addRepoWatches(repo); err != nil {
			fsw.Close()
			return nil, err
		}
	}

	go w.loop()

	return w, nil
}

// addRepoWatches registers a repo's working tree (scoped to WatchPath) and relevant .git paths.
func (w *Watcher) addRepoWatches(repo Repo) error {
	// Watch the git index/refs for changes (handles both normal repos and worktrees)
	w.watchGitDir(repo)

	// Walk only the WatchPath subtree for file changes
	return filepath.WalkDir(repo.WatchPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable dirs
		}
		if !d.IsDir() {
			return nil
		}

		// Skip .git directories
		if d.Name() == ".git" {
			return filepath.SkipDir
		}

		// Skip hidden directories (other than watch root)
		if strings.HasPrefix(d.Name(), ".") && path != repo.WatchPath {
			return filepath.SkipDir
		}

		// Skip common noisy dependency dirs
		if d.Name() == "node_modules" || d.Name() == "vendor" {
			return filepath.SkipDir
		}

		w.fsw.Add(path)
		return nil
	})
}

// watchGitDir finds and watches the actual .git directory (handling worktrees).
func (w *Watcher) watchGitDir(repo Repo) {
	gitPath := filepath.Join(repo.Path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return
	}

	if info.IsDir() {
		// Normal repo: .git is a directory
		w.fsw.Add(gitPath)
		w.watchGitRefs(gitPath)
	} else {
		// Worktree: .git is a file containing "gitdir: <path>"
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return
		}
		line := strings.TrimSpace(string(data))
		if !strings.HasPrefix(line, "gitdir: ") {
			return
		}
		gitDir := strings.TrimPrefix(line, "gitdir: ")
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(repo.Path, gitDir)
		}
		w.fsw.Add(gitDir)
		w.watchGitRefs(gitDir)
	}
}

// watchGitRefs registers .git/refs and all its subdirectories for watching.
func (w *Watcher) watchGitRefs(gitDir string) {
	refsDir := filepath.Join(gitDir, "refs")
	filepath.WalkDir(refsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			w.fsw.Add(path)
		}
		return nil
	})
}

// findRepo returns the repo that contains the given file path.
// Matches against both the repo root (for .git events) and the watch path (for file events).
func (w *Watcher) findRepo(path string) *Repo {
	for i := range w.repos {
		if strings.HasPrefix(path, w.repos[i].WatchPath+string(os.PathSeparator)) || path == w.repos[i].WatchPath {
			return &w.repos[i]
		}
		if strings.HasPrefix(path, w.repos[i].Path+string(os.PathSeparator)) || path == w.repos[i].Path {
			return &w.repos[i]
		}
	}
	return nil
}

// shouldIgnore returns true for .git internal paths that generate noise.
// Only index, HEAD, and refs/** are considered meaningful.
func (w *Watcher) shouldIgnore(path string) bool {
	repo := w.findRepo(path)
	if repo == nil {
		return true
	}

	// Check if this is inside any known git directory
	for _, gitDir := range w.resolveGitDirs(repo) {
		if strings.HasPrefix(path, gitDir+string(os.PathSeparator)) || path == gitDir {
			rel, _ := filepath.Rel(gitDir, path)
			return rel != "." && rel != "index" && rel != "HEAD" && !strings.HasPrefix(rel, "refs")
		}
	}

	return false // outside .git, don't ignore
}

// resolveGitDirs returns the git directory paths for a repo (handling worktrees).
func (w *Watcher) resolveGitDirs(repo *Repo) []string {
	var dirs []string
	gitPath := filepath.Join(repo.Path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return dirs
	}
	if info.IsDir() {
		dirs = append(dirs, gitPath)
	} else {
		// Worktree: read the gitdir pointer
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return dirs
		}
		line := strings.TrimSpace(string(data))
		if strings.HasPrefix(line, "gitdir: ") {
			gitDir := strings.TrimPrefix(line, "gitdir: ")
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(repo.Path, gitDir)
			}
			dirs = append(dirs, gitDir)
		}
	}
	return dirs
}

// loop processes fsnotify events with debouncing.
func (w *Watcher) loop() {
	for {
		select {
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}

			if w.shouldIgnore(event.Name) {
				continue
			}

			// On Create events, register new directories for watching
			// (only within watch paths, not inside .git)
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					name := filepath.Base(event.Name)
					if !strings.HasPrefix(name, ".") {
						repo := w.findRepo(event.Name)
						if repo != nil && strings.HasPrefix(event.Name, repo.WatchPath+string(os.PathSeparator)) {
							w.fsw.Add(event.Name)
						}
					}
				}
			}

			repo := w.findRepo(event.Name)
			if repo == nil {
				continue
			}

			w.mu.Lock()
			w.pending[repo.Path] = true
			if w.timer == nil {
				w.timer = time.AfterFunc(200*time.Millisecond, w.flush)
			} else {
				w.timer.Reset(200 * time.Millisecond)
			}
			w.mu.Unlock()

		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}

		case <-w.done:
			return
		}
	}
}

// flush is called when the debounce timer fires. It re-runs GetChangedFiles
// for all repos with pending changes and sends FilesChangedMsg.
func (w *Watcher) flush() {
	w.mu.Lock()
	paths := make([]string, 0, len(w.pending))
	for p := range w.pending {
		paths = append(paths, p)
	}
	w.pending = make(map[string]bool)
	w.timer = nil
	w.mu.Unlock()

	for _, path := range paths {
		repo := w.findRepo(path)
		if repo == nil {
			continue
		}
		files, err := GetChangedFiles(repo)
		if err != nil {
			continue
		}
		select {
		case w.msgCh <- FilesChangedMsg{Repo: repo, Files: files}:
		case <-w.done:
			return
		}
	}
}

// WatchRepos does an initial scan of all repos and returns the first
// FilesChangedMsg. Subsequent messages are received via WaitForChange.
func (w *Watcher) WatchRepos() tea.Cmd {
	return func() tea.Msg {
		for i := range w.repos {
			files, err := GetChangedFiles(&w.repos[i])
			if err != nil {
				continue
			}
			if len(files) > 0 {
				select {
				case w.msgCh <- FilesChangedMsg{Repo: &w.repos[i], Files: files}:
				case <-w.done:
					return nil
				}
			}
		}
		select {
		case msg := <-w.msgCh:
			return msg
		case <-w.done:
			return nil
		}
	}
}

// WaitForChange returns a tea.Cmd that blocks until the next file change
// event and returns a FilesChangedMsg.
func (w *Watcher) WaitForChange() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-w.msgCh:
			return msg
		case <-w.done:
			return nil
		}
	}
}

// Close shuts down the watcher and cleans up resources.
func (w *Watcher) Close() {
	select {
	case <-w.done:
		return // already closed
	default:
		close(w.done)
	}
	w.fsw.Close()
}
