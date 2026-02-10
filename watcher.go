package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// FilesChangedMsg is sent when a repo's changed files have been refreshed.
type FilesChangedMsg struct {
	Repo  *Repo
	Files []ChangedFile
}

// Watcher polls git repos for changes on a regular interval.
type Watcher struct {
	repos []Repo
	msgCh chan FilesChangedMsg
	done  chan struct{}
}

// NewWatcher creates a Watcher that polls the given repos for changes.
func NewWatcher(repos []Repo) (*Watcher, error) {
	w := &Watcher{
		repos: repos,
		msgCh: make(chan FilesChangedMsg, 64),
		done:  make(chan struct{}),
	}

	go w.pollLoop()

	return w, nil
}

// pollLoop periodically runs git status on all repos and sends changes.
func (w *Watcher) pollLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Track previous state to detect changes
	prev := make(map[string]string) // repo path -> concatenated file state

	for {
		select {
		case <-ticker.C:
			for i := range w.repos {
				files, err := GetChangedFiles(&w.repos[i])
				if err != nil {
					continue
				}

				// Build a fingerprint of current state
				fingerprint := fileFingerprint(files)
				if fingerprint == prev[w.repos[i].Path] {
					continue // no change
				}
				prev[w.repos[i].Path] = fingerprint

				select {
				case w.msgCh <- FilesChangedMsg{Repo: &w.repos[i], Files: files}:
				case <-w.done:
					return
				}
			}
		case <-w.done:
			return
		}
	}
}

// fileFingerprint builds a string representing the current changed-file state.
func fileFingerprint(files []ChangedFile) string {
	if len(files) == 0 {
		return ""
	}
	var b []byte
	for _, f := range files {
		b = append(b, f.Status...)
		b = append(b, ':')
		b = append(b, f.Path...)
		b = append(b, '\n')
	}
	return string(b)
}

// WaitForChange returns a tea.Cmd that blocks until the next change is detected.
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

// Close shuts down the watcher.
func (w *Watcher) Close() {
	select {
	case <-w.done:
		return // already closed
	default:
		close(w.done)
	}
}
