//go:build linux

package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// openTerminal runs command in the first terminal found on Linux, in the
// order the issue lists: the Debian x-terminal-emulator alternative, the
// user's $TERMINAL (what Hyprland and Omarchy setups set), then the common
// terminals. The command goes into a temp script the terminal's shell runs,
// so a POSIX `VAR=x prog` launch line works as-is; the shell is left open
// when the agent quits.
func openTerminal(command, choice string) error {
	f, err := os.CreateTemp("", "magpie-resume-*.sh")
	if err != nil {
		return err
	}
	script := "#!/bin/sh\n" + command + "\nexec \"${SHELL:-sh}\"\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o700)
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	scriptPath := f.Name()
	var firstErr error
	for _, bin := range linuxTerminals() {
		args := linuxTerminalArgs(bin, scriptPath)
		// Start, not Run: an xterm-style terminal lives until the user
		// closes it, and this call sits on the HTTP handler — the terminal
		// detaches, or the next candidate in the list gets a try.
		cmd := proc.Command(bin, args...)
		err := cmd.Start()
		if err == nil {
			go cmd.Wait()
			// the terminal has read it long before
			time.AfterFunc(time.Minute, func() { os.Remove(scriptPath) })
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	os.Remove(scriptPath)
	if firstErr != nil {
		return fmt.Errorf("no terminal opened the session: %w", firstErr)
	}
	return errors.New("no terminal found to open: tried " +
		strings.Join(linuxTerminals(), ", "))
}

// linuxTerminals lists the terminal binaries to try, in order.
func linuxTerminals() []string {
	terms := []string{"x-terminal-emulator"}
	if custom := strings.TrimSpace(os.Getenv("TERMINAL")); custom != "" {
		// $TERMINAL may carry a path or flags; the first word is the binary.
		terms = append(terms, strings.Fields(custom)[0])
	}
	return append(terms,
		"gnome-terminal",
		"konsole",
		"xfce4-terminal",
		"kitty",
		"alacritty",
		"foot",
	)
}

// linuxTerminalArgs makes the arguments that run `sh scriptPath` under one
// terminal: gnome-terminal dropped -e long ago (it takes -- today),
// xfce4-terminal wants -x, foot takes the command directly, and the rest of
// the list follows the xterm -e convention.
func linuxTerminalArgs(bin, scriptPath string) []string {
	var prefix []string
	switch filepath.Base(bin) {
	case "gnome-terminal":
		prefix = []string{"--"}
	case "xfce4-terminal":
		prefix = []string{"-x"}
	case "foot":
		prefix = nil
	default:
		prefix = []string{"-e"}
	}
	return append(append([]string{}, prefix...), "sh", scriptPath)
}
