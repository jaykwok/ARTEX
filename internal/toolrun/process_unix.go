//go:build !windows

package toolrun

import (
	"os/exec"
	"syscall"
)

func prepareProcess(cmd *exec.Cmd)                 { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func containProcess(cmd *exec.Cmd) (func(), error) { return func() { killProcess(cmd) }, nil }
func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
