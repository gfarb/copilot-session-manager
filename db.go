package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func isValidSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	if id == "." || id == ".." {
		return false
	}
	return sessionIDRe.MatchString(id)
}

func assertInsideStateDir(target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("abs(%s): %w", target, err)
	}
	root, err := filepath.Abs(sessionStateDir())
	if err != nil {
		return fmt.Errorf("abs(%s): %w", sessionStateDir(), err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return fmt.Errorf("rel: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to operate on %s (outside %s)", abs, root)
	}
	return nil
}

const defaultDBRelPath = ".copilot/session-store.db"

func dbPath() string {
	if p := os.Getenv("CSM_DB"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultDBRelPath
	}
	return filepath.Join(home, defaultDBRelPath)
}

func sessionStateDir() string {
	if p := os.Getenv("CSM_SESSION_STATE"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(dbPath()), "session-state")
}

func sessionDir(id string) string {
	return filepath.Join(sessionStateDir(), id)
}

func openDB() (*sql.DB, error) {
	p := dbPath()
	if _, err := os.Stat(p); err != nil {
		return nil, fmt.Errorf("session DB not found at %s: %w", p, err)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", p)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func openDBWritable() (*sql.DB, error) {
	p := dbPath()
	if _, err := os.Stat(p); err != nil {
		return nil, fmt.Errorf("session DB not found at %s: %w", p, err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)", p)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func sessionInUse(id string) bool {
	sd := sessionDir(id)
	entries, err := os.ReadDir(sd)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "inuse.") && strings.HasSuffix(n, ".lock") {
			return true
		}
	}
	return false
}

func hardDeleteSession(id string) error {
	if !isValidSessionID(id) {
		return fmt.Errorf("invalid session id %q", id)
	}
	if sessionInUse(id) {
		return fmt.Errorf("session %s is currently in use (inuse.*.lock present); refusing to delete", id)
	}

	db, err := openDBWritable()
	if err != nil {
		return fmt.Errorf("open DB for write: %w", err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if sessionInUse(id) {
		return fmt.Errorf("session %s became active during delete; aborting", id)
	}

	deletes := []struct {
		table, where string
	}{
		{"turns", "session_id = ?"},
		{"session_files", "session_id = ?"},
		{"session_refs", "session_id = ?"},
		{"checkpoints", "session_id = ?"},
		{"forge_trajectory_events", "session_id = ?"},
		{"search_index", "session_id = ?"},
		{"sessions", "id = ?"},
	}
	for _, d := range deletes {
		if _, err := tx.Exec("DELETE FROM "+d.table+" WHERE "+d.where, id); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "no such table") || strings.Contains(msg, "no such column") {
				continue
			}
			return fmt.Errorf("delete from %s: %w", d.table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	src := sessionDir(id)
	if err := assertInsideStateDir(src); err != nil {
		return err
	}
	if st, err := os.Stat(src); err == nil && st.IsDir() {
		if err := os.RemoveAll(src); err != nil {
			return fmt.Errorf("remove %s: %w", src, err)
		}
	}
	return nil
}

type sessionRow struct {
	id, cwd, repo, branch, summary, updatedAt string
	turnCount                                  int
}

func loadSessions(db *sql.DB) ([]sessionRow, error) {
	q := `
SELECT s.id,
       COALESCE(s.cwd, ''),
       COALESCE(s.repository, ''),
       COALESCE(s.branch, ''),
       COALESCE(s.summary, ''),
       COALESCE(s.updated_at, s.created_at, ''),
       (SELECT COUNT(*) FROM turns t WHERE t.session_id = s.id)
FROM sessions s
ORDER BY COALESCE(s.updated_at, s.created_at) DESC
`
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.cwd, &r.repo, &r.branch, &r.summary, &r.updatedAt, &r.turnCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadSessionExtras(db *sql.DB) (map[string]string, error) {
	extras := make(map[string]string)
	addBlob := func(id, s string) {
		if s == "" {
			return
		}
		if existing, ok := extras[id]; ok {
			extras[id] = existing + " " + s
		} else {
			extras[id] = s
		}
	}

	rows, err := db.Query(`SELECT session_id, file_path FROM session_files`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, fp string
		if err := rows.Scan(&id, &fp); err != nil {
			rows.Close()
			return nil, err
		}
		addBlob(id, fp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = db.Query(`SELECT session_id, ref_type, ref_value FROM session_refs`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, rt, rv string
		if err := rows.Scan(&id, &rt, &rv); err != nil {
			rows.Close()
			return nil, err
		}
		addBlob(id, rt+":"+rv)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = db.Query(`
SELECT session_id, COALESCE(user_message, '')
FROM turns
WHERE user_message IS NOT NULL AND user_message != ''
ORDER BY session_id, turn_index
`)
	if err != nil {
		return nil, err
	}
	turnCounts := make(map[string]int)
	for rows.Next() {
		var id, msg string
		if err := rows.Scan(&id, &msg); err != nil {
			rows.Close()
			return nil, err
		}
		if turnCounts[id] >= 10 {
			continue
		}
		turnCounts[id]++
		addBlob(id, truncate(flattenWhitespace(msg), 200))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for id, b := range extras {
		extras[id] = truncate(b, 2000)
	}
	return extras, nil
}
