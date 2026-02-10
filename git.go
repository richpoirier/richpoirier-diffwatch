package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo represents a single git repository.
type Repo struct {
	Name string // display name (relative path from discovery root, e.g. "shopify/billing")
	Path string // absolute path to repo root
}

// ChangedFile represents a file with uncommitted changes.
type ChangedFile struct {
	Repo   *Repo
	Path   string // relative to repo root
	Status string // M, A, D, R, ?, etc.
}

// DiscoverRepos walks root and returns all directories containing a .git directory.
// It stops descending into a directory once .git is found (no nested repo discovery).
// The repo display name is the path relative to root.
func DiscoverRepos(root string) ([]Repo, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var repos []Repo

	// Check if root itself is a git repo
	if isGitRepo(absRoot) {
		repos = append(repos, Repo{
			Name: filepath.Base(absRoot),
			Path: absRoot,
		})
		return repos, nil
	}

	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip directories we can't read
		}
		if !d.IsDir() {
			return nil
		}
		// Skip hidden directories (except .git which we check for)
		if d.Name() != "." && strings.HasPrefix(d.Name(), ".") && path != absRoot {
			return filepath.SkipDir
		}

		if isGitRepo(path) {
			rel, relErr := filepath.Rel(absRoot, path)
			if relErr != nil {
				rel = filepath.Base(path)
			}
			repos = append(repos, Repo{
				Name: rel,
				Path: path,
			})
			return filepath.SkipDir // don't look for nested repos
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return repos, nil
}

// isGitRepo returns true if dir contains a .git subdirectory.
func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// GetChangedFiles runs `git status --porcelain` and returns changed files for a repo.
func GetChangedFiles(repo *Repo) ([]ChangedFile, error) {
	cmd := exec.Command("git", "-C", repo.Path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []ChangedFile
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}

		// Porcelain format: XY PATH
		// X = index status, Y = worktree status
		// We use the most meaningful status character.
		xy := line[:2]
		path := strings.TrimSpace(line[3:])

		// Handle renamed files: "R  old -> new"
		if strings.Contains(path, " -> ") {
			parts := strings.SplitN(path, " -> ", 2)
			path = parts[1]
		}

		status := parseStatus(xy)
		files = append(files, ChangedFile{
			Repo:   repo,
			Path:   path,
			Status: status,
		})
	}

	return files, nil
}

// parseStatus converts the two-character porcelain status to a single display character.
func parseStatus(xy string) string {
	x := xy[0] // index (staged) status
	y := xy[1] // worktree status

	switch {
	case x == '?' || y == '?':
		return "?"
	case x == 'A' || y == 'A':
		return "A"
	case x == 'D' || y == 'D':
		return "D"
	case x == 'R' || y == 'R':
		return "R"
	case x == 'M' || y == 'M':
		return "M"
	case x == 'C' || y == 'C':
		return "C"
	default:
		return string(x)
	}
}

// GetDiff runs git diff piped through delta and returns the ANSI-colored output.
// For untracked files, it uses git diff --no-index to generate a diff.
func GetDiff(file ChangedFile) (string, error) {
	var cmd *exec.Cmd

	if file.Status == "?" {
		// Untracked file: diff against /dev/null
		absPath := filepath.Join(file.Repo.Path, file.Path)
		cmd = exec.Command("bash", "-c",
			"git -C "+shellQuote(file.Repo.Path)+
				" diff --no-index /dev/null "+shellQuote(absPath)+
				" | delta --paging=never --color-only")
	} else {
		cmd = exec.Command("bash", "-c",
			"git -C "+shellQuote(file.Repo.Path)+
				" diff -- "+shellQuote(file.Path)+
				" | delta --paging=never --color-only")
	}

	out, err := cmd.Output()
	if err != nil {
		// git diff --no-index returns exit code 1 when files differ, which is expected
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return string(out), nil
		}
		return "", err
	}

	return string(out), nil
}

// shellQuote wraps a string in single quotes for safe shell interpolation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
