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

	args := os.Args[1:]

	// Handle flags
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h":
			printUsage()
			return
		case "--list":
			listProfiles()
			return
		case "--save":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "Usage: diffwatch --save <profile-name> <path>...")
				os.Exit(1)
			}
			saveProfile(args[1], args[2:])
			return
		case "--delete":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: diffwatch --delete <profile-name>")
				os.Exit(1)
			}
			deleteProfile(args[1])
			return
		}
	}

	// Resolve paths: check if single arg is a profile name
	paths := args
	if len(paths) == 1 {
		if profilePaths := resolveProfile(paths[0]); profilePaths != nil {
			paths = profilePaths
		}
	}
	if len(paths) == 0 {
		// Try "default" profile, fall back to "."
		if profilePaths := resolveProfile("default"); profilePaths != nil {
			paths = profilePaths
		} else {
			paths = []string{"."}
		}
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

func printUsage() {
	fmt.Println(`diffwatch - watch git diffs across multiple repos

Usage:
  diffwatch [paths...]           Watch repos at the given paths
  diffwatch <profile>            Load a saved profile
  diffwatch                      Use "default" profile, or watch "."

Profiles:
  diffwatch --save <name> <path>...   Save a named profile
  diffwatch --delete <name>           Delete a profile
  diffwatch --list                    List saved profiles

Examples:
  diffwatch . ~/src/other-repo
  diffwatch --save work . ~/src/other-repo
  diffwatch work`)
}
