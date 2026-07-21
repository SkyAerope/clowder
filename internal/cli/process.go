package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/SkyAerope/clowder/internal/config"
)

func pidPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "clowder.pid"), nil
}

func readPID() (int, error) {
	p, err := pidPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// cmdStart 后台启动：自我 fork 出 serve 进程。
func cmdStart(args []string) error {
	if pid, err := readPID(); err == nil && processAlive(pid) {
		return fmt.Errorf("clowder already running (pid %d)", pid)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	d, err := config.Dir()
	if err != nil {
		return err
	}
	os.MkdirAll(d, 0o755)
	logFile, err := os.OpenFile(filepath.Join(d, "clowder.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(exe, "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("clowder started (pid %d), log: %s\n", cmd.Process.Pid, logFile.Name())
	return nil
}

// cmdStop 按 pid 文件停止。
func cmdStop(args []string) error {
	pid, err := readPID()
	if err != nil {
		return fmt.Errorf("not running (no pid file)")
	}
	if !processAlive(pid) {
		p, _ := pidPath()
		os.Remove(p)
		return fmt.Errorf("not running (stale pid %d removed)", pid)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	fmt.Printf("sent SIGTERM to pid %d\n", pid)
	return nil
}

// cmdReload 给运行中的进程发 SIGHUP。
func cmdReload(args []string) error {
	pid, err := readPID()
	if err != nil || !processAlive(pid) {
		return fmt.Errorf("not running")
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return err
	}
	fmt.Println("reload signal sent")
	return nil
}
