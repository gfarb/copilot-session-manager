package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type refRecord struct {
	refType string
	host    string
	repo    string
	value   string
	url     string
	turn    string
	ts      string
}

var (
	refURLPattern = regexp.MustCompile(
		`https?://([a-zA-Z0-9.\-]+)/([a-zA-Z0-9._\-]+)/([a-zA-Z0-9._\-]+)/(pull|issues|commit|blob|tree|edit|raw|discussions|releases|wiki)/([^\s)\]>"',#?\\]+)`)
	refGhPattern = regexp.MustCompile(
		`gh\s+(pr|issue)\s+(?:[a-z]+\s+)?(?:--repo[= ]([A-Za-z0-9._/\-]+)\s+)?(\d+)`)
	refGhPatternRepoAfter = regexp.MustCompile(
		`gh\s+(pr|issue)\s+(?:[a-z]+\s+)?(\d+)\s+--repo[= ]([A-Za-z0-9._/\-]+)`)
)

func classifyRefURLKind(kind string) string {
	switch kind {
	case "pull":
		return "pr"
	case "issues":
		return "issue"
	case "commit":
		return "commit"
	case "discussions":
		return "discussion"
	case "blob", "tree", "edit", "raw":
		return "file"
	case "releases":
		return "release"
	case "wiki":
		return "wiki"
	}
	return ""
}

func loadRefsFromEvents(sessionID, fallbackRepo string) []refRecord {
	if !isValidSessionID(sessionID) {
		return nil
	}
	seen := make(map[string]bool)
	var out []refRecord
	add := func(r refRecord) {
		key := r.refType + "|" + r.host + "|" + r.repo + "|" + r.url
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, r)
	}

	err := scanEventsJSONL(sessionID, func(line []byte) error {
		var e struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(line, &e); err != nil {
			return nil
		}

		var text, turn string
		switch e.Type {
		case "user.message":
			var d struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(e.Data, &d); err != nil {
				return nil
			}
			text = d.Content
		case "assistant.message":
			var d struct {
				Content      string `json:"content"`
				TurnID       string `json:"turnId"`
				ToolRequests []struct {
					Arguments map[string]any `json:"arguments"`
				} `json:"toolRequests"`
			}
			if err := json.Unmarshal(e.Data, &d); err != nil {
				return nil
			}
			text = d.Content
			turn = d.TurnID
			for _, tr := range d.ToolRequests {
				for _, v := range tr.Arguments {
					if s, ok := v.(string); ok {
						text += "\n" + s
					}
				}
			}
		case "tool.execution_start":
			var d struct {
				TurnID    string          `json:"turnId"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(e.Data, &d); err != nil {
				return nil
			}
			turn = d.TurnID
			text = string(d.Arguments)
		default:
			return nil
		}
		if text == "" {
			return nil
		}

		for _, m := range refURLPattern.FindAllStringSubmatch(text, -1) {
			host, owner, repo, kind, val := m[1], m[2], m[3], m[4], m[5]
			refType := classifyRefURLKind(kind)
			if refType == "" {
				continue
			}
			val = strings.TrimRight(val, ".,;:!?")
			display := val
			if refType == "commit" && len(val) > 7 {
				display = val[:7]
			}
			add(refRecord{
				refType: refType,
				host:    host,
				repo:    owner + "/" + repo,
				value:   display,
				url:     fmt.Sprintf("https://%s/%s/%s/%s/%s", host, owner, repo, kind, val),
				turn:    turn,
				ts:      e.Timestamp,
			})
		}

		addGhMatch := func(kind, repo, val string) {
			if repo == "" {
				repo = fallbackRepo
			}
			parts := strings.Split(repo, "/")
			if len(parts) != 2 {
				return
			}
			urlKind := "pull"
			refType := "pr"
			if kind == "issue" {
				urlKind = "issues"
				refType = "issue"
			}
			add(refRecord{
				refType: refType,
				host:    "github.com",
				repo:    repo,
				value:   val,
				url:     fmt.Sprintf("https://github.com/%s/%s/%s/%s", parts[0], parts[1], urlKind, val),
				turn:    turn,
				ts:      e.Timestamp,
			})
		}
		for _, m := range refGhPattern.FindAllStringSubmatch(text, -1) {
			addGhMatch(m[1], m[2], m[3])
		}
		for _, m := range refGhPatternRepoAfter.FindAllStringSubmatch(text, -1) {
			addGhMatch(m[1], m[3], m[2])
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "csm: warning: events.jsonl read for %s failed: %v\n", sessionID, err)
	}
	return out
}

func loadRefsFromDB(db *sql.DB, id string) []refRecord {
	rows, err := db.Query(`
SELECT ref_type, ref_value, COALESCE(turn_index, -1), COALESCE(created_at, '')
FROM session_refs WHERE session_id = ? ORDER BY created_at ASC, id ASC`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []refRecord
	for rows.Next() {
		var rt, rv, ts string
		var turn int
		if err := rows.Scan(&rt, &rv, &turn, &ts); err != nil {
			continue
		}
		t := ""
		if turn >= 0 {
			t = fmt.Sprintf("%d", turn)
		}
		out = append(out, refRecord{
			refType: rt, value: rv, turn: t, ts: ts,
		})
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "csm: warning: session_refs query for %s: %v\n", id, err)
	}
	return out
}

func loadAllRefs(db *sql.DB, id string) []refRecord {
	repo := ""
	_ = db.QueryRow(`SELECT COALESCE(repository,'') FROM sessions WHERE id = ?`, id).Scan(&repo)
	refs := loadRefsFromEvents(id, repo)
	if len(refs) == 0 {
		refs = loadRefsFromDB(db, id)
	}
	return refs
}

func renderRefs(db *sql.DB, id string) (string, error) {
	refs := loadAllRefs(db, id)
	if len(refs) == 0 {
		return "REFS\n  (none)\n", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "REFS (%d)\n", len(refs))
	maxType := 4
	for _, r := range refs {
		if l := len(r.refType); l > maxType {
			maxType = l
		}
	}
	for _, r := range refs {
		turn := "-"
		if r.turn != "" {
			turn = r.turn
			if !strings.HasPrefix(turn, "t") {
				turn = "t" + turn
			}
		}
		label := r.value
		if r.repo != "" {
			sep := "#"
			switch r.refType {
			case "commit":
				sep = "@"
			case "file", "release", "wiki":
				sep = ":"
			}
			label = r.repo + sep + r.value
		}
		if r.url != "" {
			fmt.Fprintf(&b, "  %-*s  %-5s  %s  %s  %s\n",
				maxType, r.refType, turn, shortTime(r.ts), label, r.url)
		} else {
			fmt.Fprintf(&b, "  %-*s  %-5s  %s  %s\n",
				maxType, r.refType, turn, shortTime(r.ts), label)
		}
	}
	return b.String(), nil
}
