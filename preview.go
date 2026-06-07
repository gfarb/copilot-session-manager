package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func flattenWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func shortTime(ts string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
	}
	if len(ts) >= 16 {
		return strings.Replace(ts[:16], "T", " ", 1)
	}
	return ts
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func displayLine(r sessionRow) string {
	repo := r.repo
	if repo == "" {
		repo = filepath.Base(r.cwd)
		if repo == "" || repo == "." || repo == "/" {
			repo = "(no-repo)"
		}
	}
	summary := r.summary
	if summary == "" {
		summary = "(no summary)"
	}
	branch := ""
	if r.branch != "" {
		branch = "@" + r.branch
	}
	return fmt.Sprintf("%s  [%s%s]  %s  (%d turns)",
		shortTime(r.updatedAt), repo, branch, summary, r.turnCount)
}

func renderPreview(db *sql.DB, id string) (string, error) {
	overview, err := renderOverview(db, id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(overview)

	if files, _ := renderFiles(db, id); strings.TrimSpace(files) != "" {
		b.WriteString("\n")
		b.WriteString(files)
	}
	if refs, _ := renderRefs(db, id); strings.TrimSpace(refs) != "" {
		b.WriteString("\n")
		b.WriteString(refs)
	}
	return b.String(), nil
}

func renderOverview(db *sql.DB, id string) (string, error) {
	var s sessionRow
	err := db.QueryRow(`
SELECT id,
       COALESCE(cwd, ''),
       COALESCE(repository, ''),
       COALESCE(branch, ''),
       COALESCE(summary, ''),
       COALESCE(updated_at, created_at, ''),
       (SELECT COUNT(*) FROM turns t WHERE t.session_id = sessions.id)
FROM sessions WHERE id = ?`, id).Scan(&s.id, &s.cwd, &s.repo, &s.branch, &s.summary, &s.updatedAt, &s.turnCount)
	if err != nil {
		return "", fmt.Errorf("session %s not found: %w", id, err)
	}

	var createdAt string
	_ = db.QueryRow(`SELECT COALESCE(created_at, '') FROM sessions WHERE id = ?`, id).Scan(&createdAt)

	var b strings.Builder
	fmt.Fprintf(&b, "id        %s\n", s.id)
	if s.repo != "" {
		fmt.Fprintf(&b, "repo      %s\n", s.repo)
	}
	if s.branch != "" {
		fmt.Fprintf(&b, "branch    %s\n", s.branch)
	}
	if s.cwd != "" {
		fmt.Fprintf(&b, "cwd       %s\n", s.cwd)
	}
	fmt.Fprintf(&b, "created   %s\n", shortTime(createdAt))
	fmt.Fprintf(&b, "updated   %s\n", shortTime(s.updatedAt))
	fmt.Fprintf(&b, "turns     %d\n", s.turnCount)
	if s.summary != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "SUMMARY")
		fmt.Fprintln(&b, "  "+s.summary)
	}

	var firstMsg string
	_ = db.QueryRow(`
SELECT COALESCE(user_message, '')
FROM turns
WHERE session_id = ? AND user_message IS NOT NULL AND user_message != ''
ORDER BY turn_index ASC LIMIT 1`, id).Scan(&firstMsg)
	if firstMsg != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "FIRST USER MESSAGE")
		fmt.Fprintln(&b, indent(truncate(firstMsg, 1000), "  "))
	}

	var lastMsg string
	_ = db.QueryRow(`
SELECT COALESCE(user_message, '')
FROM turns
WHERE session_id = ? AND user_message IS NOT NULL AND user_message != ''
ORDER BY turn_index DESC LIMIT 1`, id).Scan(&lastMsg)
	if lastMsg != "" && lastMsg != firstMsg {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "LAST USER MESSAGE")
		fmt.Fprintln(&b, indent(truncate(lastMsg, 600), "  "))
	}
	return b.String(), nil
}

func renderTurns(db *sql.DB, id string) (string, error) {
	rows, err := db.Query(`
SELECT turn_index, COALESCE(user_message, ''), COALESCE(assistant_response, ''), COALESCE(timestamp, '')
FROM turns
WHERE session_id = ?
ORDER BY turn_index ASC`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type turnRow struct {
		idx            int
		user, asst, ts string
	}
	var turns []turnRow
	for rows.Next() {
		var t turnRow
		if err := rows.Scan(&t.idx, &t.user, &t.asst, &t.ts); err != nil {
			return "", err
		}
		turns = append(turns, t)
	}
	if len(turns) == 0 {
		return "TURNS\n  (none)\n", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "TURNS (%d)\n", len(turns))
	for _, t := range turns {
		fmt.Fprintf(&b, "\nt%d  %s\n", t.idx, shortTime(t.ts))
		if t.user != "" {
			fmt.Fprintf(&b, "  user: %s\n", truncate(flattenWhitespace(t.user), 300))
		}
		if t.asst != "" {
			fmt.Fprintf(&b, "  asst: %s\n", truncate(flattenWhitespace(t.asst), 300))
		}
	}
	return b.String(), nil
}
