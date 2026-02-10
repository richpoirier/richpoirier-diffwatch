package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Panel identifies which panel has focus.
type Panel int

const (
	// LeftPanel is the file tree panel.
	LeftPanel Panel = iota
	// RightPanel is the diff view panel.
	RightPanel
)

// Model is the root bubbletea model that owns layout and dispatches to sub-models.
type Model struct {
	filetree FileTreeModel
	diffview DiffViewModel
	focus    Panel
	width    int
	height   int
	splitPos float64 // 0.0 to 1.0, default 0.3
	repos    []Repo
	watcher  *Watcher
}

// NewModel creates a new root model with the given repos and watcher.
func NewModel(repos []Repo, watcher *Watcher) Model {
	return Model{
		filetree: NewFileTreeModel(),
		diffview: NewDiffViewModel(),
		focus:    LeftPanel,
		splitPos: 0.3,
		repos:    repos,
		watcher:  watcher,
	}
}

// Init implements tea.Model. Does initial file scan and starts listening for changes.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.initialScan(), m.watcher.WaitForChange())
}

// initialScan runs GetChangedFiles for all repos and sends results through the watcher channel.
func (m *Model) initialScan() tea.Cmd {
	return func() tea.Msg {
		for i := range m.repos {
			files, err := GetChangedFiles(&m.repos[i])
			if err != nil || len(files) == 0 {
				continue
			}
			return FilesChangedMsg{
				Repo:  &m.repos[i],
				Files: files,
			}
		}
		return nil
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateSizes()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.filetree.filtering {
				// Let filetree handle 'q' during filter mode
				break
			}
			return m, tea.Quit
		case "tab":
			if m.focus == LeftPanel {
				m.focus = RightPanel
			} else {
				m.focus = LeftPanel
			}
			return m, nil
		case "r":
			if !m.filetree.filtering {
				return m, m.refreshAll()
			}
		}

		// Delegate to focused panel
		if m.focus == LeftPanel {
			var cmd tea.Cmd
			m.filetree, cmd = m.filetree.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.diffview, cmd = m.diffview.Update(msg)
		return m, cmd

	case FilesChangedMsg:
		m.filetree, _ = m.filetree.Update(msg)

		// Auto-select first file if nothing is selected
		if m.filetree.selected == nil {
			items := m.filetree.visibleItems()
			for _, item := range items {
				if !item.isRepo {
					files := m.filetree.filteredFiles(item.repoIndex)
					if item.fileIndex < len(files) {
						file := files[item.fileIndex]
						m.filetree.selected = &file
						m.diffview.SetLoading()
						return m, tea.Batch(
							loadDiff(file),
							m.watcher.WaitForChange(),
						)
					}
				}
			}
		}

		return m, m.watcher.WaitForChange()

	case FileSelectedMsg:
		m.diffview.SetLoading()
		return m, loadDiff(msg.File)

	case DiffLoadedMsg:
		m.diffview, _ = m.diffview.Update(msg)
		return m, nil
	}

	return m, nil
}

// refreshAll re-scans all repos and returns the first result as a message.
func (m *Model) refreshAll() tea.Cmd {
	return func() tea.Msg {
		for i := range m.repos {
			files, err := GetChangedFiles(&m.repos[i])
			if err != nil {
				continue
			}
			return FilesChangedMsg{
				Repo:  &m.repos[i],
				Files: files,
			}
		}
		return nil
	}
}

// updateSizes recalculates sub-model dimensions.
func (m *Model) updateSizes() {
	leftWidth := int(float64(m.width) * m.splitPos)
	rightWidth := m.width - leftWidth - 3 // 3 for borders/divider
	contentHeight := m.height - 4         // borders + header

	if leftWidth < 10 {
		leftWidth = 10
	}
	if rightWidth < 10 {
		rightWidth = 10
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	m.filetree.SetSize(leftWidth, contentHeight)
	m.diffview.SetSize(rightWidth, contentHeight)
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	leftWidth := int(float64(m.width) * m.splitPos)
	rightWidth := m.width - leftWidth - 3
	contentHeight := m.height - 4

	if leftWidth < 10 {
		leftWidth = 10
	}
	if rightWidth < 10 {
		rightWidth = 10
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Border styles
	focusedBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12"))
	unfocusedBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8"))

	// Left panel
	leftTitle := fmt.Sprintf(" Changed Files (%d) ", m.filetree.totalFileCount())
	leftStyle := unfocusedBorder
	if m.focus == LeftPanel {
		leftStyle = focusedBorder
	}
	leftPanel := leftStyle.
		Width(leftWidth).
		Height(contentHeight).
		Render(m.filetree.View())

	// Right panel
	rightTitle := " Diff "
	if m.diffview.filePath != "" {
		rightTitle = fmt.Sprintf(" %s ", m.diffview.filePath)
	}
	rightStyle := unfocusedBorder
	if m.focus == RightPanel {
		rightStyle = focusedBorder
	}
	rightPanel := rightStyle.
		Width(rightWidth).
		Height(contentHeight).
		Render(m.diffview.View())

	// Add titles to border tops
	_ = leftTitle
	_ = rightTitle

	// Join panels horizontally
	content := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Status bar
	statusStyle := lipgloss.NewStyle().
		Faint(true).
		PaddingLeft(1)
	focusName := "file tree"
	if m.focus == RightPanel {
		focusName = "diff view"
	}
	repoCount := len(m.repos)
	status := statusStyle.Render(
		fmt.Sprintf("%d repo(s) | focus: %s | tab:switch  r:refresh  q:quit",
			repoCount, focusName))

	return content + "\n" + truncateToWidth(status, m.width)
}

// truncateToWidth cuts a string to fit within the given width.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = line[:width]
		}
	}
	return strings.Join(lines, "\n")
}
