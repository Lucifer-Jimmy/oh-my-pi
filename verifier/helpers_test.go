package verifier

import (
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

func commandShell(t *testing.T) tools.Shell {
	t.Helper()
	sh := tools.ResolveShell("")
	if sh.Path == "" {
		t.Fatal("no shell available")
	}
	return sh
}

func posix(sh tools.Shell) bool {
	return sh.Name != "cmd" && sh.Name != "powershell" && sh.Name != "pwsh"
}

func failCommand(sh tools.Shell) string {
	if posix(sh) {
		return "echo boom; exit 1"
	}
	return "Write-Output boom; exit 1"
}

func sleepCommand(sh tools.Shell, seconds int) string {
	if posix(sh) {
		return "sleep " + itoa(seconds)
	}
	return "Start-Sleep -Seconds " + itoa(seconds)
}

func noisyCommand(sh tools.Shell) string {
	if posix(sh) {
		return "yes x | head -c 200000; exit 1"
	}
	return "for ($i = 0; $i -lt 4000; $i++) { 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' }; exit 1"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
