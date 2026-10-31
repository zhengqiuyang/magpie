//go:build windows

package gui

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/proc"
)

// The .cmd file runs under cmd.exe, so the launch line's POSIX env
// assignments must become set statements, and a value that spells shell
// commands must land inside the quoted set value, never as a second command.
func TestWindowsScriptRewritesLaunchLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "quoted env value with spaces and metacharacters",
			in:   `NOTE='$(calc) & more' "C:\bin\x.exe" --flag`,
			want: "set \"NOTE=$(calc) & more\"\r\n\"C:\\bin\\x.exe\" --flag\r\n",
		},
		{
			name: "embedded single quote survives the POSIX escape",
			in:   `MSG='it'\''s' "C:\bin\x.exe"`,
			want: "set \"MSG=it's\"\r\n\"C:\\bin\\x.exe\"\r\n",
		},
		{
			name: "unquoted env value",
			in:   `PORT=8787 "C:\bin\x.exe"`,
			want: "set \"PORT=8787\"\r\n\"C:\\bin\\x.exe\"\r\n",
		},
		{
			name: "several assignments",
			in:   `A='1' B='2 2' "C:\bin\x.exe"`,
			want: "set \"A=1\"\r\nset \"B=2 2\"\r\n\"C:\\bin\\x.exe\"\r\n",
		},
		{
			name: "no assignments, quoted program line stays",
			in:   `"C:\my dir\x.exe" --flag`,
			want: "\"C:\\my dir\\x.exe\" --flag\r\n",
		},
		{
			name: "no assignments, plain program line stays",
			in:   "C:\\bin\\x.exe --flag",
			want: "C:\\bin\\x.exe --flag\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowsScript(tt.in)
			if err != nil {
				t.Fatalf("windowsScript(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("windowsScript(%q)\n = %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// A line of nothing but assignments has no program to run.
func TestWindowsScriptRejectsProgramlessLine(t *testing.T) {
	if _, err := windowsScript(`VAR='x'`); err == nil {
		t.Error("windowsScript(`VAR='x'`) should fail: nothing to run")
	}
}

// Only real environment names introduce an assignment: otherwise a program
// argument like `a=b` would be eaten.
func TestCutEnvAssignmentStopsAtNonEnvNames(t *testing.T) {
	if _, _, _, ok := cutEnvAssignment("1BAD=x prog"); ok {
		t.Error("a name starting with a digit is not an env assignment")
	}
	if _, _, _, ok := cutEnvAssignment("A-B=x prog"); ok {
		t.Error("a name with a dash is not an env assignment")
	}
	key, value, rest, ok := cutEnvAssignment("PATH='x' prog")
	if !ok || key != "PATH" || value != "x" || rest != "prog" {
		t.Errorf("cutEnvAssignment(PATH assignment) = %q, %q, %q, %v", key, value, rest, ok)
	}
}

// agentCommand quotes the program for cmd.exe on Windows: a metacharacter
// path becomes one double-quoted word, so it runs as a program and nothing
// else.
func TestAgentCommandQuotesForCmd(t *testing.T) {
	dir := t.TempDir() + `\it's a $(calc) & dir ^more`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prog := dir + `\claude.exe`
	if err := os.WriteFile(prog, []byte("rem fake agent\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	proc.Forget()
	t.Cleanup(proc.Forget)

	got := agentCommand(&agent.Agent{WSL: "", Bin: prog})
	// one double-quoted word, so cmd.exe runs the program and nothing else
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Errorf("agentCommand should double-quote the program path, got %q", got)
	}
}

// A WSL agent's line reaches its distro through wsl.exe, with the program's
// distro-side path (BinPath is "" for one in a WSL distro).
func TestAgentCommandWSLLine(t *testing.T) {
	got := agentCommand(&agent.Agent{WSL: "Ubuntu-22.04", Bin: "/usr/local/bin/claude"})
	want := "wsl.exe -d Ubuntu-22.04 -- /usr/local/bin/claude"
	if got != want {
		t.Errorf("agentCommand(WSL agent) = %q, want %q", got, want)
	}
	if got := agentCommand(&agent.Agent{WSL: "Ubuntu-22.04"}); got != "" {
		t.Errorf("agentCommand(WSL agent with no Bin) = %q, want empty", got)
	}
}

// The script windowsScript makes runs under cmd.exe for real: env values
// with metacharacters survive, and a % doubles back to one. The script lives
// in a directory with spaces, like a user profile's can.
func TestWindowsScriptRunsUnderCmd(t *testing.T) {
	dir := t.TempDir() + `\spaced out dir`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := dir + `\out.txt`
	// The child prints its own NOTE, so no % reference goes through the
	// batch round-trip — a POSIX launch line carries none either.
	script, err := windowsScript(`NOTE='a & b 100%' "C:\Windows\System32\cmd.exe" /c set NOTE > "` + out + `"`)
	if err != nil {
		t.Fatal(err)
	}
	script = strings.ReplaceAll(script, "%", "%%")
	path := dir + `\resume.cmd`
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("cmd", "/c", path).Run(); err != nil {
		t.Fatalf("cmd /c %s: %v", path, err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "a & b 100%") {
		t.Errorf("the note did not survive the batch round-trip: %q", string(got))
	}
}
