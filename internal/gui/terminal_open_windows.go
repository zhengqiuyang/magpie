//go:build windows

package gui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/proc"
)

// openTerminal runs command in a terminal on Windows: Windows Terminal
// (wt.exe) when it is installed, else `cmd /c start`, falling back to
// PowerShell. The command is a launch line made by agentCommand, so its
// leading POSIX `VAR=x` assignments are rewritten into `set` statements and
// the whole line goes into a .cmd file the Windows shell runs natively; the
// shell is left open when the agent quits.
func openTerminal(command, choice string) error {
	script, err := windowsScript(command)
	if err != nil {
		return err
	}
	// A batch file doubles % so the line's own percent signs survive.
	script = strings.ReplaceAll(script, "%", "%%")
	f, err := os.CreateTemp("", "magpie-resume-*.cmd")
	if err != nil {
		return err
	}
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	// wt opens its own window; cmd's start does the same for the fallback,
	// and /k keeps the console open on the script so the shell survives the
	// agent quitting.
	if _, err := exec.LookPath("wt.exe"); err == nil {
		err = proc.Command("wt.exe", "cmd", "/k", f.Name()).Run()
	} else {
		err = proc.Command("cmd", "/c", "start", "", "cmd", "/k", f.Name()).Run()
		if err != nil {
			err = proc.Command("powershell", "-NoProfile", "-Command",
				"Start-Process", "-FilePath", f.Name()).Run()
		}
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	// the terminal has read it long before
	time.AfterFunc(time.Minute, func() { os.Remove(f.Name()) })
	return nil
}

// windowsScript rewrites a POSIX launch line into cmd.exe statements: each
// leading `VAR=value` assignment becomes a `set "VAR=value"` line and the
// program line follows. The program itself is already quoted for cmd.exe by
// agentCommand (windowsQuote), not with the POSIX single quotes a .cmd file
// would run as text.
func windowsScript(command string) (string, error) {
	rest := strings.TrimSpace(command)
	var b strings.Builder
	for {
		key, value, tail, ok := cutEnvAssignment(rest)
		if !ok {
			break
		}
		fmt.Fprintf(&b, "set \"%s=%s\"\r\n", key, value)
		rest = tail
	}
	if rest == "" {
		return "", fmt.Errorf("no program in %q", command)
	}
	b.WriteString(rest + "\r\n")
	return b.String(), nil
}

// cutEnvAssignment splits one leading POSIX `VAR=value` assignment off a
// launch line. The value may be single-quoted (shellQuote), with the shell's
// '\” escape for embedded quotes.
func cutEnvAssignment(line string) (key, value, rest string, ok bool) {
	eq := strings.Index(line, "=")
	if eq <= 0 {
		return "", "", "", false
	}
	key = line[:eq]
	if !isEnvName(key) {
		return "", "", "", false
	}
	rest = line[eq+1:]
	if !strings.HasPrefix(rest, "'") {
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			return key, rest, "", true
		}
		return key, rest[:sp], strings.TrimLeft(rest[sp+1:], " "), true
	}
	rest = rest[1:]
	value = ""
	for {
		i := strings.Index(rest, "'")
		if i < 0 {
			return "", "", "", false
		}
		if strings.HasPrefix(rest[i:], `'\''`) {
			value += rest[:i] + "'"
			rest = rest[i+4:]
			continue
		}
		value += rest[:i]
		rest = rest[i+1:]
		break
	}
	return key, value, strings.TrimLeft(rest, " "), true
}

// isEnvName is whether name can be an environment variable's: letters,
// digits and underscores, not starting with a digit.
func isEnvName(name string) bool {
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return name != ""
}
