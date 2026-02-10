package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Check delta is available
	if _, err := exec.LookPath("delta"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: 'delta' is not installed or not on PATH.")
		fmt.Fprintln(os.Stderr, "Install it with: brew install git-delta")
		os.Exit(1)
	}

	// Parse args: paths to watch (default: ".")
	paths := os.Args[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}

	// Discover repos from all paths
	var allRepos []Repo
	for _, path := range paths {
		repos, err := DiscoverRepos(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not scan %s: %v\n", path, err)
			continue
		}
		allRepos = append(allRepos, repos...)
	}

	if len(allRepos) == 0 {
		fmt.Fprintln(os.Stderr, "No git repositories found in the specified paths.")
		os.Exit(1)
	}

	fmt.Printf("Found %d repo(s), starting diffwatch...\n", len(allRepos))

	// Start watcher
	watcher, err := NewWatcher(allRepos)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting file watcher: %v\n", err)
		os.Exit(1)
	}
	defer watcher.Close()

	// Start TUI
	model := NewModel(allRepos, watcher)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
