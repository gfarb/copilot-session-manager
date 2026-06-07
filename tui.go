package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	focusList = iota
	focusPreview
)

type previewTab int

const (
	tabOverview previewTab = iota
	tabFiles
	tabRefs
	tabTurns
)

var tabNames = []string{"Overview", "Files", "Refs", "Turns"}
var numTabs = previewTab(len(tabNames))

type tuiModel struct {
	db            *sql.DB
	list          list.Model
	viewport      viewport.Model
	filesList     list.Model
	refsList      list.Model
	width         int
	height        int
	focus         int
	tab           previewTab
	showHelp      bool
	flash         string
	confirmPrompt string
	confirmAction func(m *tuiModel) tea.Cmd
	resumeID      string
	resumeCwd     string
	printOnly     bool
	quitWith      string
	lastID        string
	lastTab       previewTab
}

func newTUIModel(db *sql.DB, sessions []sessionRow, extras map[string]string) tuiModel {
	items := make([]list.Item, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, sessionItem{row: s, extras: extras[s.id]})
	}
	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 0, 0)
	l.Title = fmt.Sprintf("Copilot sessions (%d)", len(sessions))
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)

	fl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	fl.Title = "Files"
	fl.SetShowStatusBar(false)
	fl.SetFilteringEnabled(true)
	fl.SetShowHelp(false)

	rl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	rl.Title = "Refs"
	rl.SetShowStatusBar(false)
	rl.SetFilteringEnabled(true)
	rl.SetShowHelp(false)

	vp := viewport.New(0, 0)
	return tuiModel{
		db:        db,
		list:      l,
		viewport:  vp,
		filesList: fl,
		refsList:  rl,
		focus:     focusList,
		tab:       tabOverview,
		lastTab:   -1,
	}
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if c := m.relayout(); c != nil {
			cmds = append(cmds, c)
		}

	case tea.KeyMsg:
		if m.confirmPrompt != "" {
			key := msg.String()
			action := m.confirmAction
			m.confirmPrompt = ""
			m.confirmAction = nil
			if key == "y" || key == "Y" {
				if action != nil {
					return m, action(&m)
				}
				return m, nil
			}
			m.flash = "cancelled"
			return m, nil
		}
		if m.list.FilterState() == list.Filtering {
			break
		}
		if m.tab == tabFiles && m.filesList.FilterState() == list.Filtering {
			break
		}
		if m.tab == tabRefs && m.refsList.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, m.relayout()
		case "enter":
			if item, ok := m.list.SelectedItem().(sessionItem); ok {
				if m.printOnly {
					m.quitWith = item.row.cwd + "\t" + item.row.id + "\n"
				} else {
					m.resumeID = item.row.id
					m.resumeCwd = item.row.cwd
				}
				return m, tea.Quit
			}
		case "c":
			if item, ok := m.list.SelectedItem().(sessionItem); ok {
				m.setFlash(copyToClipboard(item.row.id), "copied id: "+item.row.id)
			}
			return m, nil
		case "C":
			if item, ok := m.list.SelectedItem().(sessionItem); ok && item.row.cwd != "" {
				m.setFlash(copyToClipboard(item.row.cwd), "copied cwd: "+item.row.cwd)
			}
			return m, nil
		case "tab":
			if m.focus == focusList {
				m.focus = focusPreview
			} else {
				m.focus = focusList
			}
			return m, nil
		case "d":
			if item, ok := m.list.SelectedItem().(sessionItem); ok {
				sid := item.row.id
				summary := item.row.summary
				if summary == "" {
					summary = "(no summary)"
				}
				if len([]rune(summary)) > 40 {
					summary = string([]rune(summary)[:40]) + "…"
				}
				short := sid
				if len(short) > 8 {
					short = short[:8]
				}
				m.confirmPrompt = fmt.Sprintf(
					"HARD-delete session %q (id %s)? Removes DB rows + folder. NO UNDO. Press y to confirm.",
					summary, short)
				m.confirmAction = func(mm *tuiModel) tea.Cmd {
					if err := hardDeleteSession(sid); err != nil {
						mm.flash = "delete failed: " + err.Error()
						return nil
					}
					mm.flash = "deleted: " + sid
					items := mm.list.Items()
					kept := make([]list.Item, 0, len(items))
					for _, it := range items {
						si, ok := it.(sessionItem)
						if ok && si.row.id == sid {
							continue
						}
						kept = append(kept, it)
					}
					setItemsCmd := mm.list.SetItems(kept)
					mm.list.Title = fmt.Sprintf("Copilot sessions (%d)", len(kept))
					mm.lastID = ""
					return tea.Batch(setItemsCmd, mm.refreshPreview())
				}
				return m, nil
			}
		case "1":
			m.tab = tabOverview
			return m, m.refreshPreview()
		case "2":
			m.tab = tabFiles
			return m, m.refreshPreview()
		case "3":
			m.tab = tabRefs
			return m, m.refreshPreview()
		case "4":
			m.tab = tabTurns
			return m, m.refreshPreview()
		case "]", "shift+right":
			m.tab = (m.tab + 1) % numTabs
			return m, m.refreshPreview()
		case "[", "shift+left":
			m.tab = (m.tab - 1 + numTabs) % numTabs
			return m, m.refreshPreview()
		case "o":
			if rec, ok := m.selectedFile(); ok {
				m.setFlash(openWith("open", rec.path), "opened: "+rec.path)
				return m, nil
			}
			if rec, ok := m.selectedRef(); ok && rec.url != "" {
				m.setFlash(openWith("open", rec.url), "opened: "+rec.url)
				return m, nil
			}
		case "O":
			if rec, ok := m.selectedFile(); ok {
				dir := filepath.Dir(rec.path)
				m.setFlash(openWith("open", dir), "opened dir: "+dir)
				return m, nil
			}
		case "v":
			if rec, ok := m.selectedFile(); ok {
				m.setFlash(openWith("code", rec.path), "opened in code: "+rec.path)
				return m, nil
			}
		case "y":
			if rec, ok := m.selectedFile(); ok {
				m.setFlash(copyToClipboard(rec.path), "copied path: "+rec.path)
				return m, nil
			}
			if rec, ok := m.selectedRef(); ok && rec.url != "" {
				m.setFlash(copyToClipboard(rec.url), "copied url: "+rec.url)
				return m, nil
			}
		case "e":
			if rec, ok := m.selectedFile(); ok {
				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vi"
				}
				cmd := exec.Command(editor, rec.path)
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					if err != nil {
						return editorFinishedMsg{err: err, path: rec.path}
					}
					return editorFinishedMsg{path: rec.path}
				})
			}
		}

	case editorFinishedMsg:
		if msg.err != nil {
			m.flash = "editor failed: " + msg.err.Error()
		} else {
			m.flash = "edited: " + msg.path
		}
		return m, nil
	}

	if _, isKey := msg.(tea.KeyMsg); isKey {
		m.flash = ""
	}

	if m.focus == focusList {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.tab == tabFiles {
		var cmd tea.Cmd
		m.filesList, cmd = m.filesList.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.tab == tabRefs {
		var cmd tea.Cmd
		m.refsList, cmd = m.refsList.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	if item, ok := m.list.SelectedItem().(sessionItem); ok && (item.row.id != m.lastID || m.tab != m.lastTab) {
		m.lastID = item.row.id
		m.lastTab = m.tab
		if c := m.refreshPreview(); c != nil {
			cmds = append(cmds, c)
		}
	}

	return m, tea.Batch(cmds...)
}

type editorFinishedMsg struct {
	err  error
	path string
}

func (m *tuiModel) selectedFile() (fileRecord, bool) {
	if m.tab != tabFiles {
		return fileRecord{}, false
	}
	item, ok := m.filesList.SelectedItem().(fileItem)
	if !ok {
		return fileRecord{}, false
	}
	return item.rec, true
}

func (m *tuiModel) selectedRef() (refRecord, bool) {
	if m.tab != tabRefs {
		return refRecord{}, false
	}
	item, ok := m.refsList.SelectedItem().(refItem)
	if !ok {
		return refRecord{}, false
	}
	return item.rec, true
}

func (m *tuiModel) setFlash(err error, success string) {
	if err != nil {
		m.flash = "error: " + err.Error()
	} else {
		m.flash = success
	}
}

func (m *tuiModel) relayout() tea.Cmd {
	if m.width == 0 || m.height == 0 {
		return nil
	}
	statusH := 1
	if m.showHelp {
		statusH = 3
	}
	if m.flash != "" || m.confirmPrompt != "" {
		statusH++
	}
	contentH := m.height - statusH - 2
	if contentH < 5 {
		contentH = 5
	}
	totalW := m.width
	leftBoxW := totalW * 45 / 100
	rightBoxW := totalW - leftBoxW
	leftContentW := leftBoxW - 2
	rightContentW := rightBoxW - 2
	if leftContentW < 20 {
		leftContentW = 20
	}
	if rightContentW < 20 {
		rightContentW = 20
	}
	m.list.SetSize(leftContentW, contentH)
	innerH := contentH - 2
	if innerH < 3 {
		innerH = 3
	}
	m.viewport.Width = rightContentW
	m.viewport.Height = innerH
	m.filesList.SetSize(rightContentW, innerH)
	m.refsList.SetSize(rightContentW, innerH)
	return m.refreshPreview()
}

func (m *tuiModel) refreshPreview() tea.Cmd {
	item, ok := m.list.SelectedItem().(sessionItem)
	if !ok {
		m.viewport.SetContent("")
		var cmds []tea.Cmd
		cmds = append(cmds, m.filesList.SetItems(nil))
		cmds = append(cmds, m.refsList.SetItems(nil))
		return tea.Batch(cmds...)
	}
	if m.tab == tabFiles {
		records := loadAllFiles(m.db, item.row.id)
		items := make([]list.Item, len(records))
		for i, r := range records {
			items[i] = fileItem{rec: r, cwd: item.row.cwd}
		}
		cmd := m.filesList.SetItems(items)
		m.filesList.Title = fmt.Sprintf("Files (%d)", len(records))
		return cmd
	}
	if m.tab == tabRefs {
		records := loadAllRefs(m.db, item.row.id)
		items := make([]list.Item, len(records))
		for i, r := range records {
			items[i] = refItem{rec: r}
		}
		cmd := m.refsList.SetItems(items)
		m.refsList.Title = fmt.Sprintf("Refs (%d)", len(records))
		return cmd
	}
	var (
		content string
		err     error
	)
	switch m.tab {
	case tabOverview:
		content, err = renderOverview(m.db, item.row.id)
	case tabTurns:
		content, err = renderTurns(m.db, item.row.id)
	}
	if err != nil {
		content = fmt.Sprintf("error: %v", err)
	}
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
	return nil
}

var (
	inactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("241"))
	activeBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("212"))
	statusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	flashStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	confirmStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214")).Bold(true).Padding(0, 1)
	tabActiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Padding(0, 1)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Padding(0, 1)
)

func (m tuiModel) renderTabBar(width int) string {
	var parts []string
	for i, name := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if previewTab(i) == m.tab {
			parts = append(parts, tabActiveStyle.Render(label))
		} else {
			parts = append(parts, tabInactiveStyle.Render(label))
		}
	}
	bar := strings.Join(parts, " ")
	if lipgloss.Width(bar) > width {
		bar = lipgloss.NewStyle().MaxWidth(width).Render(bar)
	}
	return bar
}

func (m tuiModel) View() string {
	if m.width == 0 {
		return "loading..."
	}
	leftStyle := inactiveBorder
	rightStyle := inactiveBorder
	if m.focus == focusList {
		leftStyle = activeBorder
	} else {
		rightStyle = activeBorder
	}
	left := leftStyle.Width(m.list.Width()).Height(m.list.Height()).Render(m.list.View())

	tabBar := m.renderTabBar(m.viewport.Width)
	var paneBody string
	switch m.tab {
	case tabFiles:
		paneBody = m.filesList.View()
	case tabRefs:
		paneBody = m.refsList.View()
	default:
		paneBody = m.viewport.View()
	}
	rightInner := tabBar + "\n" + strings.Repeat("─", m.viewport.Width) + "\n" + paneBody
	rightHeight := m.viewport.Height + 2
	right := rightStyle.Width(m.viewport.Width).Height(rightHeight).Render(rightInner)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	var status string
	focusLabel := "list"
	if m.focus == focusPreview {
		focusLabel = "preview"
	}
	if m.showHelp {
		status = statusStyle.Render(
			"↑/↓ j/k navigate · / filter · enter resume · d delete · c copy id · C copy cwd · tab swap focus",
		) + "\n" + statusStyle.Render(
			"1-4 tabs · [/] cycle · Files: o open · O dir · v code · e $EDITOR · y copy path · q quit · focus: "+focusLabel,
		)
	} else {
		hint := "? help · enter resume · d delete · / filter · 1-4 tabs · q quit"
		switch m.tab {
		case tabFiles:
			hint = "? help · o open · O dir · v code · e $EDITOR · y copy path · d delete session · / filter · q quit"
		case tabRefs:
			hint = "? help · o open url · y copy url · d delete session · / filter · q quit"
		}
		status = statusStyle.Render(fmt.Sprintf("[%s] %s", focusLabel, hint))
	}
	if m.confirmPrompt != "" {
		status = confirmStyle.Render(m.confirmPrompt) + "\n" + status
	} else if m.flash != "" {
		status = flashStyle.Render(m.flash) + "\n" + status
	}
	return body + "\n" + status
}

func runTUI(printOnly bool) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	sessions, err := loadSessions(db)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "no sessions found")
		return nil
	}
	extras, err := loadSessionExtras(db)
	if err != nil {
		return err
	}
	m := newTUIModel(db, sessions, extras)
	m.printOnly = printOnly
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithOutput(os.Stderr),
	)
	final, err := p.Run()
	if err != nil {
		return err
	}
	fm, ok := final.(tuiModel)
	if !ok {
		return nil
	}
	if fm.resumeID != "" {
		return resumeSession(fm.resumeCwd, fm.resumeID)
	}
	if fm.quitWith != "" {
		fmt.Print(fm.quitWith)
	}
	return nil
}
