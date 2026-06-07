package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type fileRecord struct {
	category string
	path     string
	tool     string
	turn     string
	ts       string
	size     int64
}

var fileTools = map[string]bool{
	"view": true, "edit": true, "create": true, "read": true, "write": true,
	"str_replace_editor": true,
}

func loadSessionArtifacts(id string) []fileRecord {
	var out []fileRecord
	sd := sessionDir(id)

	if st, err := os.Stat(filepath.Join(sd, "plan.md")); err == nil && !st.IsDir() {
		out = append(out, fileRecord{
			category: "plan",
			path:     filepath.Join(sd, "plan.md"),
			ts:       st.ModTime().UTC().Format(time.RFC3339),
			size:     st.Size(),
		})
	}

	for _, sub := range []struct {
		dir string
		cat string
	}{{"files", "artifact"}, {"checkpoints", "checkpoint"}} {
		entries, err := os.ReadDir(filepath.Join(sd, sub.dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, fileRecord{
				category: sub.cat,
				path:     filepath.Join(sd, sub.dir, e.Name()),
				ts:       info.ModTime().UTC().Format(time.RFC3339),
				size:     info.Size(),
			})
		}
	}
	return out
}

func scanEventsJSONL(id string, fn func(line []byte) error) error {
	f, err := os.Open(filepath.Join(sessionDir(id), "events.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 {
				if cbErr := fn([]byte(line)); cbErr != nil {
					return cbErr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func loadWorkspaceFilesFromEvents(id string) []fileRecord {
	if !isValidSessionID(id) {
		return nil
	}
	var out []fileRecord
	seen := make(map[string]bool)
	err := scanEventsJSONL(id, func(line []byte) error {
		var e struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Data      struct {
				ToolName  string          `json:"toolName"`
				TurnID    string          `json:"turnId"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"data"`
		}
		if err := json.Unmarshal(line, &e); err != nil {
			return nil
		}
		if e.Type != "tool.execution_start" || !fileTools[e.Data.ToolName] {
			return nil
		}
		if len(e.Data.Arguments) == 0 {
			return nil
		}
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(e.Data.Arguments, &args); err != nil || args.Path == "" {
			return nil
		}
		if seen[args.Path] {
			return nil
		}
		seen[args.Path] = true
		out = append(out, fileRecord{
			category: "workspace",
			path:     args.Path,
			tool:     e.Data.ToolName,
			turn:     e.Data.TurnID,
			ts:       e.Timestamp,
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "csm: warning: events.jsonl read for %s failed: %v\n", id, err)
	}
	return out
}

func loadWorkspaceFilesFromDB(db *sql.DB, id string) []fileRecord {
	rows, err := db.Query(`
SELECT file_path, COALESCE(tool_name, ''), COALESCE(turn_index, -1), COALESCE(first_seen_at, '')
FROM session_files WHERE session_id = ? ORDER BY first_seen_at ASC, id ASC`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []fileRecord
	for rows.Next() {
		var p, tn, ts string
		var turn int
		if err := rows.Scan(&p, &tn, &turn, &ts); err != nil {
			continue
		}
		t := ""
		if turn >= 0 {
			t = fmt.Sprintf("%d", turn)
		}
		out = append(out, fileRecord{category: "workspace", path: p, tool: tn, turn: t, ts: ts})
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "csm: warning: session_files query for %s: %v\n", id, err)
	}
	return out
}

func humanSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/1024/1024)
	}
}

func loadWorkspaceFiles(db *sql.DB, id string) []fileRecord {
	ws := loadWorkspaceFilesFromEvents(id)
	if len(ws) == 0 {
		ws = loadWorkspaceFilesFromDB(db, id)
	}
	prefix := sessionDir(id) + string(os.PathSeparator)
	out := ws[:0]
	for _, r := range ws {
		if strings.HasPrefix(r.path, prefix) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func loadAllFiles(db *sql.DB, id string) []fileRecord {
	artifacts := loadSessionArtifacts(id)
	workspace := loadWorkspaceFiles(db, id)

	var plan, files, checkpoints []fileRecord
	for _, r := range artifacts {
		switch r.category {
		case "plan":
			plan = append(plan, r)
		case "artifact":
			files = append(files, r)
		case "checkpoint":
			checkpoints = append(checkpoints, r)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ts > files[j].ts })
	sort.Slice(checkpoints, func(i, j int) bool { return checkpoints[i].ts > checkpoints[j].ts })

	out := make([]fileRecord, 0, len(plan)+len(files)+len(checkpoints)+len(workspace))
	out = append(out, plan...)
	out = append(out, files...)
	out = append(out, checkpoints...)
	out = append(out, workspace...)
	return out
}

func renderFiles(db *sql.DB, id string) (string, error) {
	artifacts := loadSessionArtifacts(id)
	workspace := loadWorkspaceFiles(db, id)

	if len(artifacts) == 0 && len(workspace) == 0 {
		return "FILES\n  (none)\n", nil
	}

	var plan, files, checkpoints []fileRecord
	for _, r := range artifacts {
		switch r.category {
		case "plan":
			plan = append(plan, r)
		case "artifact":
			files = append(files, r)
		case "checkpoint":
			checkpoints = append(checkpoints, r)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ts > files[j].ts })
	sort.Slice(checkpoints, func(i, j int) bool { return checkpoints[i].ts > checkpoints[j].ts })

	var b strings.Builder

	if len(plan) > 0 {
		fmt.Fprintln(&b, "PLAN")
		for _, r := range plan {
			fmt.Fprintf(&b, "  %s  (%s, %s)\n", r.path, humanSize(r.size), shortTime(r.ts))
		}
		fmt.Fprintln(&b)
	}

	if len(files) > 0 {
		fmt.Fprintf(&b, "SESSION ARTIFACTS (%d)\n", len(files))
		for _, r := range files {
			fmt.Fprintf(&b, "  %s  (%s, %s)\n", r.path, humanSize(r.size), shortTime(r.ts))
		}
		fmt.Fprintln(&b)
	}

	if len(checkpoints) > 0 {
		fmt.Fprintf(&b, "CHECKPOINTS (%d)\n", len(checkpoints))
		for _, r := range checkpoints {
			fmt.Fprintf(&b, "  %s  (%s, %s)\n", r.path, humanSize(r.size), shortTime(r.ts))
		}
		fmt.Fprintln(&b)
	}

	if len(workspace) > 0 {
		fmt.Fprintf(&b, "WORKSPACE FILES (%d)\n", len(workspace))
		maxTool := 4
		for _, r := range workspace {
			if l := len(r.tool); l > maxTool {
				maxTool = l
			}
		}
		for _, r := range workspace {
			tool := r.tool
			if tool == "" {
				tool = "-"
			}
			turn := r.turn
			if turn == "" {
				turn = "-"
			} else if !strings.HasPrefix(turn, "t") {
				turn = "t" + turn
			}
			fmt.Fprintf(&b, "  %-*s  %-5s  %s  %s\n",
				maxTool+2, "["+tool+"]", turn, shortTime(r.ts), r.path)
		}
	}

	return b.String(), nil
}
