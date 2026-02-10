package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FileSelectedMsg is sent when the user selects a file in the tree.
type FileSelectedMsg struct {
	File ChangedFile
}

// RepoGroup represents a repo and its changed files in the tree view.
type RepoGroup struct {
	Repo      *Repo
	Files     []ChangedFile
	Collapsed bool
}

// FileTreeModel is the left panel showing a navigable file tree grouped by repo.
type FileTreeModel struct {
	repos    []RepoGroup
	cursor   int          // index into flattened visible items
	selected *ChangedFile // currently selected file
	width    int
	height   int
	filter   string
	filtering bool
}

// NewFileTreeModel creates a new FileTreeModel.
func NewFileTreeModel() FileTreeModel {
	return FileTreeModel{}
}

// flatItem represents a single row in the flattened tree view.
type flatItem struct {
	isRepo    bool
	repoIndex int
	fileIndex int // -1 for repo headers
}

// visibleItems returns the flattened list of currently visible items.
func (m *FileTreeModel) visibleItems() []flatItem {
	var items []flatItem
	for ri, rg := range m.repos {
		// Skip repos with no files matching filter
		if m.filter != "" && len(m.filteredFiles(ri)) == 0 {
			continue
		}
		items = append(items, flatItem{isRepo: true, repoIndex: ri, fileIndex: -1})
		if !rg.Collapsed {
			files := m.filteredFiles(ri)
			for fi := range files {
				items = append(items, flatItem{isRepo: false, repoIndex: ri, fileIndex: fi})
			}
		}
	}
	return items
}

// filteredFiles returns files matching the current filter for a repo.
func (m *FileTreeModel) filteredFiles(repoIndex int) []ChangedFile {
	if m.filter == "" {
		return m.repos[repoIndex].Files
	}
	var filtered []ChangedFile
	for _, f := range m.repos[repoIndex].Files {
		if strings.Contains(strings.ToLower(f.Path), strings.ToLower(m.filter)) {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// totalFileCount returns the total number of changed files across all repos.
func (m *FileTreeModel) totalFileCount() int {
	count := 0
	for _, rg := range m.repos {
		count += len(rg.Files)
	}
	return count
}

// Init implements tea.Model.
func (m FileTreeModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m FileTreeModel) Update(msg tea.Msg) (FileTreeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case FilesChangedMsg:
		return m.handleFilesChanged(msg)

	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilter(msg)
		}
		return m.updateNavigation(msg)
	}
	return m, nil
}

func (m FileTreeModel) updateFilter(msg tea.KeyMsg) (FileTreeModel, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.filtering = false
		if msg.String() == "esc" {
			m.filter = ""
		}
		m.clampCursor()
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
		}
		m.clampCursor()
	default:
		if len(msg.String()) == 1 {
			m.filter += msg.String()
			m.clampCursor()
		}
	}
	return m, nil
}

func (m FileTreeModel) updateNavigation(msg tea.KeyMsg) (FileTreeModel, tea.Cmd) {
	items := m.visibleItems()
	if len(items) == 0 {
		return m, nil
	}

	switch msg.String() {
	case "j", "down":
		if m.cursor < len(items)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if m.cursor < len(items) {
			item := items[m.cursor]
			if item.isRepo {
				m.repos[item.repoIndex].Collapsed = !m.repos[item.repoIndex].Collapsed
				m.clampCursor()
			} else {
				files := m.filteredFiles(item.repoIndex)
				if item.fileIndex < len(files) {
					file := files[item.fileIndex]
					m.selected = &file
					return m, func() tea.Msg {
						return FileSelectedMsg{File: file}
					}
				}
			}
		}
	case "c":
		if m.cursor < len(items) {
			item := items[m.cursor]
			ri := item.repoIndex
			m.repos[ri].Collapsed = !m.repos[ri].Collapsed
			m.clampCursor()
		}
	case "/":
		m.filtering = true
		m.filter = ""
	}

	return m, nil
}

// handleFilesChanged updates the tree with new file data for a repo.
func (m FileTreeModel) handleFilesChanged(msg FilesChangedMsg) (FileTreeModel, tea.Cmd) {
	found := false
	for i, rg := range m.repos {
		if rg.Repo.Path == msg.Repo.Path {
			m.repos[i].Files = msg.Files
			found = true
			break
		}
	}
	if !found && len(msg.Files) > 0 {
		m.repos = append(m.repos, RepoGroup{
			Repo:  msg.Repo,
			Files: msg.Files,
		})
	}

	// Prune repos with no remaining files
	kept := m.repos[:0]
	for _, rg := range m.repos {
		if len(rg.Files) > 0 {
			kept = append(kept, rg)
		}
	}
	m.repos = kept

	// Clear selection if the selected file is no longer in the changed set
	if m.selected != nil {
		stillExists := false
		for _, rg := range m.repos {
			for _, f := range rg.Files {
				if f.Repo.Path == m.selected.Repo.Path && f.Path == m.selected.Path {
					stillExists = true
					break
				}
			}
			if stillExists {
				break
			}
		}
		if !stillExists {
			m.selected = nil
		}
	}

	m.clampCursor()

	// Auto-select first file if nothing is selected
	if m.selected == nil {
		items := m.visibleItems()
		for _, item := range items {
			if !item.isRepo {
				files := m.filteredFiles(item.repoIndex)
				if item.fileIndex < len(files) {
					file := files[item.fileIndex]
					m.selected = &file
					return m, func() tea.Msg {
						return FileSelectedMsg{File: file}
					}
				}
			}
		}
	}

	return m, nil
}

// clampCursor ensures cursor stays within bounds.
func (m *FileTreeModel) clampCursor() {
	items := m.visibleItems()
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// SetSize sets the available width and height for rendering.
func (m *FileTreeModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// View implements tea.Model.
func (m FileTreeModel) View() string {
	items := m.visibleItems()

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selectedStyle := lipgloss.NewStyle().Reverse(true)
	statusColors := map[string]lipgloss.Style{
		"M": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),  // yellow
		"A": lipgloss.NewStyle().Foreground(lipgloss.Color("2")),  // green
		"D": lipgloss.NewStyle().Foreground(lipgloss.Color("1")),  // red
		"R": lipgloss.NewStyle().Foreground(lipgloss.Color("6")),  // cyan
		"?": lipgloss.NewStyle().Foreground(lipgloss.Color("8")),  // gray
	}

	if len(items) == 0 {
		msg := "No uncommitted changes found.\nWatching for changes..."
		if m.filter != "" {
			msg = fmt.Sprintf("No files matching '%s'", m.filter)
		}
		return lipgloss.NewStyle().
			Faint(true).
			Padding(1, 2).
			Render(msg)
	}

	var lines []string
	maxLines := m.height
	if maxLines <= 0 {
		maxLines = 50
	}

	// Calculate scroll offset to keep cursor visible
	scrollOffset := 0
	if m.cursor >= maxLines {
		scrollOffset = m.cursor - maxLines + 1
	}

	for i, item := range items {
		if i < scrollOffset {
			continue
		}
		if len(lines) >= maxLines {
			break
		}

		var line string
		if item.isRepo {
			rg := m.repos[item.repoIndex]
			arrow := "▾"
			if rg.Collapsed {
				arrow = "▸"
			}
			fileCount := len(m.filteredFiles(item.repoIndex))
			line = headerStyle.Render(fmt.Sprintf("%s %s (%d)", arrow, rg.Repo.Name, fileCount))
		} else {
			files := m.filteredFiles(item.repoIndex)
			if item.fileIndex < len(files) {
				f := files[item.fileIndex]
				statusStyle, ok := statusColors[f.Status]
				if !ok {
					statusStyle = lipgloss.NewStyle()
				}
				line = fmt.Sprintf("  %s %s", statusStyle.Render(f.Status), f.Path)
			}
		}

		// Fit line to panel width (truncate or pad)
		if m.width > 0 {
			line = lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(line)
		}

		if i == m.cursor {
			line = selectedStyle.Render(line)
		}

		lines = append(lines, line)
	}

	result := strings.Join(lines, "\n")

	// Show filter bar at bottom
	if m.filtering {
		filterBar := fmt.Sprintf("/%s█", m.filter)
		result += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(filterBar)
	}

	return result
}
