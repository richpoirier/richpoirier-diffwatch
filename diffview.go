package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DiffLoadedMsg is sent when a diff has been loaded for a file.
type DiffLoadedMsg struct {
	File    ChangedFile
	Content string // ANSI string from delta
	Err     error
}

// DiffViewModel is the right panel showing a scrollable, syntax-highlighted diff.
type DiffViewModel struct {
	viewport viewport.Model
	filePath string // currently displayed file path for header
	loading  bool
	width    int
	height   int
	lines    []string // split content for hunk navigation
}

// NewDiffViewModel creates a new DiffViewModel.
func NewDiffViewModel() DiffViewModel {
	vp := viewport.New(0, 0)
	return DiffViewModel{
		viewport: vp,
	}
}

// Init implements tea.Model.
func (m DiffViewModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m DiffViewModel) Update(msg tea.Msg) (DiffViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case DiffLoadedMsg:
		m.loading = false
		if msg.Err != nil {
			m.viewport.SetContent(lipgloss.NewStyle().
				Foreground(lipgloss.Color("1")).
				Render("Error loading diff: " + msg.Err.Error()))
			m.lines = nil
			return m, nil
		}
		m.filePath = msg.File.Path
		m.viewport.SetContent(msg.Content)
		m.viewport.GotoTop()
		m.lines = strings.Split(msg.Content, "\n")
		return m, nil

	case tea.KeyMsg:
		return m.updateKeys(msg)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m DiffViewModel) updateKeys(msg tea.KeyMsg) (DiffViewModel, tea.Cmd) {
	switch msg.String() {
	case "g":
		m.viewport.GotoTop()
		return m, nil
	case "G":
		m.viewport.GotoBottom()
		return m, nil
	case "d", "ctrl+d":
		m.viewport.HalfViewDown()
		return m, nil
	case "u", "ctrl+u":
		m.viewport.HalfViewUp()
		return m, nil
	case "n":
		m.jumpToNextHunk()
		return m, nil
	case "N":
		m.jumpToPrevHunk()
		return m, nil
	}

	// Default: let viewport handle j/k/up/down scrolling
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// jumpToNextHunk moves the viewport to the next @@ hunk header after the current position.
func (m *DiffViewModel) jumpToNextHunk() {
	if m.lines == nil {
		return
	}
	currentLine := m.viewport.YOffset
	for i := currentLine + 1; i < len(m.lines); i++ {
		if strings.Contains(m.lines[i], "@@") {
			m.viewport.SetYOffset(i)
			return
		}
	}
}

// jumpToPrevHunk moves the viewport to the previous @@ hunk header before the current position.
func (m *DiffViewModel) jumpToPrevHunk() {
	if m.lines == nil {
		return
	}
	currentLine := m.viewport.YOffset
	for i := currentLine - 1; i >= 0; i-- {
		if strings.Contains(m.lines[i], "@@") {
			m.viewport.SetYOffset(i)
			return
		}
	}
}

// SetSize sets the available width and height for the viewport.
func (m *DiffViewModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = h
}

// SetLoading marks the diff view as loading.
func (m *DiffViewModel) SetLoading() {
	m.loading = true
}

// Clear resets the diff view to an empty state.
func (m *DiffViewModel) Clear() {
	m.filePath = ""
	m.loading = false
	m.viewport.SetContent("")
	m.lines = nil
}

// View implements tea.Model.
func (m DiffViewModel) View() string {
	if m.loading {
		return lipgloss.NewStyle().
			Faint(true).
			Padding(1, 2).
			Render("Loading...")
	}

	if m.filePath == "" {
		return lipgloss.NewStyle().
			Faint(true).
			Padding(1, 2).
			Render("Select a file to view diff")
	}

	return m.viewport.View()
}

// loadDiff returns a tea.Cmd that loads the diff for a file asynchronously.
func loadDiff(file ChangedFile) tea.Cmd {
	return func() tea.Msg {
		content, err := GetDiff(file)
		return DiffLoadedMsg{
			File:    file,
			Content: content,
			Err:     err,
		}
	}
}
