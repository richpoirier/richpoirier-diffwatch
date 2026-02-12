package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Repo represents a single git repository.
type Repo struct {
	Name      string // display name (relative path from discovery root, e.g. "shopify/billing")
	Path      string // absolute path to repo root
	WatchPath string // absolute path to the subtree to watch (may equal Path)
}

// ChangedFile represents a file with uncommitted changes.
type ChangedFile struct {
	Repo   *Repo
	Path   string // relative to repo root
	Status string // M, A, D, R, ?, etc.
}

// DiscoverRepos finds git repos starting from root. If root is inside a git repo
// (or is one), it returns that repo with WatchPath scoped to root. Otherwise it
// walks down looking for repos.
func DiscoverRepos(root string) ([]Repo, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var repos []Repo

	// Check if root itself is a git repo
	if isGitRepo(absRoot) {
		repos = append(repos, Repo{
			Name:      filepath.Base(absRoot),
			Path:      absRoot,
			WatchPath: absRoot,
		})
		return repos, nil
	}

	// Check if root is inside a git repo (walk up to find repo root)
	if repoRoot := findGitRoot(absRoot); repoRoot != "" {
		name := filepath.Base(repoRoot)
		// Include the subdirectory in the display name
		rel, _ := filepath.Rel(repoRoot, absRoot)
		if rel != "." {
			name = name + "/" + rel
		}
		repos = append(repos, Repo{
			Name:      name,
			Path:      repoRoot,
			WatchPath: absRoot,
		})
		return repos, nil
	}

	// Walk down looking for repos
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
				Name:      rel,
				Path:      path,
				WatchPath: path,
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

// isGitRepo returns true if dir contains a .git entry (directory or worktree file).
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// findGitRoot walks up from dir looking for a .git entry, returning the repo root
// or "" if not found. Stops at filesystem root.
func findGitRoot(dir string) string {
	for {
		if isGitRepo(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // reached filesystem root
		}
		dir = parent
	}
}

// GetChangedFiles runs `git status --porcelain` and returns changed files for a repo.
// When WatchPath is a subdirectory of the repo root, only files under that subtree are returned.
func GetChangedFiles(repo *Repo) ([]ChangedFile, error) {
	args := []string{"-C", repo.Path, "--no-optional-locks", "status", "--porcelain", "--untracked-files=all"}
	// Scope git status to the watch subtree for large repos
	if repo.WatchPath != repo.Path {
		rel, err := filepath.Rel(repo.Path, repo.WatchPath)
		if err == nil {
			args = append(args, "--", rel)
		}
	}
	cmd := exec.Command("git", args...)
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

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

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
				" --no-optional-locks diff --no-index /dev/null "+shellQuote(absPath)+
				" | delta --paging=never --color-only --line-numbers --file-style=omit --hunk-header-style=omit")
	} else {
		cmd = exec.Command("bash", "-c",
			"git -C "+shellQuote(file.Repo.Path)+
				" --no-optional-locks diff -- "+shellQuote(file.Path)+
				" | delta --paging=never --color-only --line-numbers --file-style=omit --hunk-header-style=omit")
	}

	out, err := cmd.Output()
	if err != nil {
		// git diff --no-index returns exit code 1 when files differ, which is expected
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return stripDiffHeader(string(out)), nil
		}
		return "", err
	}

	return stripDiffHeader(string(out)), nil
}

// stripDiffHeader removes the git diff frontmatter (diff --git, index, mode, ---/+++ lines)
// from the beginning of the output.
func stripDiffHeader(s string) string {
	lines := strings.Split(s, "\n")
	start := 0
	for start < len(lines) {
		plain := stripAnsi(lines[start])
		if strings.HasPrefix(plain, "diff --git ") ||
			strings.HasPrefix(plain, "index ") ||
			strings.HasPrefix(plain, "--- ") ||
			strings.HasPrefix(plain, "+++ ") ||
			strings.HasPrefix(plain, "new file mode") ||
			strings.HasPrefix(plain, "old mode") ||
			strings.HasPrefix(plain, "new mode") ||
			strings.HasPrefix(plain, "deleted file mode") ||
			strings.HasPrefix(plain, "similarity index") ||
			strings.HasPrefix(plain, "rename from") ||
			strings.HasPrefix(plain, "rename to") ||
			plain == "" {
			start++
			continue
		}
		break
	}
	return strings.Join(lines[start:], "\n")
}

// stripAnsi removes ANSI escape sequences from a string for comparison purposes.
func stripAnsi(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && s[j] != 'm' {
					j++
				}
				if j < len(s) {
					j++
				}
			}
			i = j
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// shellQuote wraps a string in single quotes for safe shell interpolation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
