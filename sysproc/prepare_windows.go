//go:build windows

package sysproc

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

func Prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Cancel = func() error {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
		if kill.Run() != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
