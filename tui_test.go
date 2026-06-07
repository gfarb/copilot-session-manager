package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestModelRender(t *testing.T) {
	db, err := openDB()
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	defer db.Close()
	sessions, err := loadSessions(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) == 0 {
		t.Skip("no sessions in DB")
	}
	extras, _ := loadSessionExtras(db)

	m := newTUIModel(db, sessions, extras)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = tm.(tuiModel)

	view := m.View()
	if view == "" || view == "loading..." {
		t.Fatalf("view is empty/loading")
	}

	if !strings.Contains(view, "Copilot sessions") {
		t.Errorf("list title missing")
	}
	if !strings.Contains(view, "filter") {
		t.Errorf("status bar missing 'filter'")
	}

	first := sessions[0]
	hits := 0
	for _, marker := range []string{"id ", "cwd ", "SUMMARY", first.id[:8]} {
		if strings.Contains(view, marker) {
			hits++
		}
	}
	if hits == 0 {
		t.Errorf("no preview content rendered. View tail:\n%s", view[len(view)-1500:])
	}

	// Simulate enter: should mark a resume.
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := tm.(tuiModel)
	if m2.resumeID == "" {
		t.Errorf("enter did not populate resumeID")
	}
	if m2.resumeID != first.id {
		t.Errorf("resumeID %q != first.id %q", m2.resumeID, first.id)
	}
	if cmd == nil {
		t.Errorf("enter did not return a quit cmd")
	}

	// Enter with printOnly=true should populate quitWith instead.
	m.printOnly = true
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := tm.(tuiModel)
	if m3.quitWith == "" || !strings.Contains(m3.quitWith, first.id) {
		t.Errorf("printOnly enter did not produce quitWith with id; got %q", m3.quitWith)
	}

	// 'q' should produce quit cmd.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("q did not return quit cmd")
	}
}

func TestFilesTabAndOpenAction(t *testing.T) {
	db, err := openDB()
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	defer db.Close()
	sessions, err := loadSessions(db)
	if err != nil || len(sessions) == 0 {
		t.Skip("no sessions")
	}
	extras, _ := loadSessionExtras(db)

	// Find a session that has files.
	var found *sessionRow
	for i := range sessions {
		if len(loadAllFiles(db, sessions[i].id)) > 0 {
			found = &sessions[i]
			break
		}
	}
	if found == nil {
		t.Skip("no session with any files")
	}

	m := newTUIModel(db, []sessionRow{*found}, extras)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = tm.(tuiModel)

	// Switch to Files tab.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = tm.(tuiModel)
	if m.tab != tabFiles {
		t.Fatalf("expected tabFiles, got %v", m.tab)
	}
	if len(m.filesList.Items()) == 0 {
		t.Fatalf("filesList empty after switching to Files tab")
	}

	// View should render the filesList.
	view := m.View()
	if !strings.Contains(view, "Files (") {
		t.Errorf("view missing files list title; got tail:\n%s", view[max(0, len(view)-800):])
	}
	if !strings.Contains(view, "[") {
		t.Errorf("view missing any file item (no '[' badges); got tail:\n%s", view[max(0, len(view)-800):])
	}
	firstFile := m.filesList.Items()[0].(fileItem).rec.path

	// selectedFile should return the first item.
	rec, ok := m.selectedFile()
	if !ok {
		t.Fatalf("selectedFile returned !ok on Files tab")
	}
	if rec.path != firstFile {
		t.Errorf("selectedFile path mismatch: got %q want %q", rec.path, firstFile)
	}

	// 'y' should set a flash.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = tm.(tuiModel)
	if !strings.Contains(m.flash, "copied path") {
		t.Errorf("expected 'copied path' flash, got %q", m.flash)
	}

	// Back to Overview: selectedFile should return !ok.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = tm.(tuiModel)
	if _, ok := m.selectedFile(); ok {
		t.Errorf("selectedFile returned ok off the Files tab")
	}
}
