package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	printOnly := false
	args := os.Args[1:]
	filtered := args[:0]
	for _, a := range args {
		switch a {
		case "--print", "-p":
			printOnly = true
		default:
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if len(args) > 0 {
		switch args[0] {
		case "preview":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "usage: csm preview <session-id>")
				os.Exit(2)
			}
			db, err := openDB()
			if err != nil {
				fail(err)
			}
			defer db.Close()
			out, err := renderPreview(db, args[1])
			if err != nil {
				fail(err)
			}
			fmt.Print(out)
			return
		case "list":
			if err := cmdList(); err != nil {
				fail(err)
			}
			return
		case "-h", "--help", "help":
			printHelp()
			return
		default:
			fmt.Fprintln(os.Stderr, "unknown command:", args[0])
			printHelp()
			os.Exit(2)
		}
	}
	if err := runTUI(printOnly); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func printHelp() {
	fmt.Print(`csm

Usage:
  csm            Launch the TUI. On <enter>, resume the
                                     selected session via copilot --resume=<id>.
  csm --print    Print "<cwd>\t<id>" on <enter> instead of
                                     resuming (for shell scripting).
  csm list       Print TSV: "id\tdisplay\tsearch-blob".
  csm preview <id>
                                     Print a human-readable session detail page.
  csm -h         This help.

Env:
  CSM_DB             Override path to session-store.db
                     (default: ~/.copilot/session-store.db)
  CSM_SESSION_STATE  Override path to session-state directory
                     (default: <CSM_DB-dir>/session-state)

TUI keys:
  ↑/↓ j/k    navigate                 /          filter
  enter      resume session           c          copy session id
  C          copy session cwd         tab        swap focus (list <-> preview)
  1-4        jump tab                 [/]        cycle tabs
  ?          toggle help              q ctrl-c   quit

Files tab keys (when tab 2 is active):
  o  open file (macOS ` + "`open`" + `)         O  open parent directory in Finder
  v  open in VS Code (` + "`code`" + `)          e  open in $EDITOR (TUI suspends)
  y  copy file path to clipboard
`)
}

func cmdList() error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	sessions, err := loadSessions(db)
	if err != nil {
		return err
	}
	extras, err := loadSessionExtras(db)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		blob := extras[s.id]
		searchable := strings.Join([]string{s.repo, s.branch, s.cwd, s.summary, blob}, " ")
		display := flattenWhitespace(displayLine(s))
		searchable = flattenWhitespace(searchable)
		line := s.id + "\t" + display + "\t" + searchable + "\n"
		if _, err := io.WriteString(os.Stdout, line); err != nil {
			return err
		}
	}
	return nil
}
