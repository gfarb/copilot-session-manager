package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

func resumeSession(cwd, id string) error {
	bin, err := exec.LookPath("copilot")
	if err != nil {
		return fmt.Errorf("copilot CLI not found in PATH (install it or use `csm --print`): %w", err)
	}
	if cwd != "" {
		if err := os.Chdir(cwd); err != nil {
			return fmt.Errorf("chdir to %s: %w (refusing to resume in wrong directory)", cwd, err)
		}
	}
	args := []string{bin, "--resume=" + id}
	return syscall.Exec(bin, args, os.Environ())
}

func copyToClipboard(s string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard tool found (install wl-copy, xclip, or xsel)")
		}
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func openWith(program string, args ...string) error {
	if _, err := exec.LookPath(program); err != nil {
		return fmt.Errorf("%s not found in PATH", program)
	}
	if len(args) > 0 && !looksLikeURL(args[0]) {
		if _, err := os.Stat(args[0]); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("not found: %s", args[0])
			}
			return fmt.Errorf("stat %s: %w", args[0], err)
		}
	}
	cmd := exec.Command(program, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s: %s", program, msg)
		}
		return fmt.Errorf("%s: %w", program, err)
	}
	return nil
}

func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
