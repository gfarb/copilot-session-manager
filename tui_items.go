package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sessionItem struct {
	row    sessionRow
	extras string
}

func (i sessionItem) Title() string {
	repo := i.row.repo
	if repo == "" {
		repo = "(no-repo)"
	}
	summary := i.row.summary
	if summary == "" {
		summary = "(no summary)"
	}
	if i.row.branch != "" {
		repo = repo + "@" + i.row.branch
	}
	return fmt.Sprintf("%s · %s", repo, summary)
}

func (i sessionItem) Description() string {
	cwd := i.row.cwd
	if cwd == "" {
		cwd = "-"
	}
	return fmt.Sprintf("%s · %d turns · %s",
		shortTime(i.row.updatedAt), i.row.turnCount, cwd)
}

func (i sessionItem) FilterValue() string {
	return strings.Join([]string{
		i.row.summary, i.row.repo, i.row.branch, i.row.cwd, i.extras,
	}, " ")
}

func displayPath(p, cwd string) string {
	if p == "" {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if cwd != "" {
		if absCwd, err := filepath.Abs(cwd); err == nil && absCwd != string(os.PathSeparator) {
			if rel, err := filepath.Rel(absCwd, abs); err == nil &&
				!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
				if rel == "." {
					return "./"
				}
				return "./" + rel
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, abs); err == nil &&
			!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
			return "~/" + rel
		}
	}
	return abs
}

type fileItem struct {
	rec fileRecord
	cwd string
}

func (i fileItem) Title() string {
	badge := "[" + i.rec.category + "]"
	var path string
	switch i.rec.category {
	case "plan":
		path = "plan.md"
	case "artifact":
		path = "files/" + filepath.Base(i.rec.path)
	case "checkpoint":
		path = "checkpoints/" + filepath.Base(i.rec.path)
	default:
		if i.rec.category == "workspace" && i.rec.tool != "" {
			badge = "[" + i.rec.tool + "]"
		}
		path = displayPath(i.rec.path, i.cwd)
	}
	return fmt.Sprintf("%s %s", badge, path)
}

func (i fileItem) Description() string {
	parts := []string{i.rec.category}
	if i.rec.category == "workspace" {
		turn := i.rec.turn
		if turn != "" {
			if !strings.HasPrefix(turn, "t") {
				turn = "t" + turn
			}
			parts = append(parts, turn)
		}
	} else if i.rec.size > 0 {
		parts = append(parts, humanSize(i.rec.size))
	}
	if ts := shortTime(i.rec.ts); ts != "" {
		parts = append(parts, ts)
	}
	return strings.Join(parts, " · ")
}

func (i fileItem) FilterValue() string {
	return i.rec.path + " " + i.rec.category + " " + i.rec.tool
}

type refItem struct{ rec refRecord }

func (i refItem) Title() string {
	badge := "[" + i.rec.refType + "]"
	label := i.rec.value
	if i.rec.repo != "" {
		sep := "#"
		switch i.rec.refType {
		case "commit":
			sep = "@"
		case "file", "release", "wiki":
			sep = ":"
		}
		label = i.rec.repo + sep + i.rec.value
	}
	return fmt.Sprintf("%s %s", badge, label)
}

func (i refItem) Description() string {
	parts := []string{}
	if i.rec.turn != "" {
		turn := i.rec.turn
		if !strings.HasPrefix(turn, "t") {
			turn = "t" + turn
		}
		parts = append(parts, turn)
	}
	if ts := shortTime(i.rec.ts); ts != "" {
		parts = append(parts, ts)
	}
	if i.rec.url != "" {
		parts = append(parts, i.rec.url)
	}
	return strings.Join(parts, " · ")
}

func (i refItem) FilterValue() string {
	return i.rec.refType + " " + i.rec.repo + " " + i.rec.value + " " + i.rec.url
}
