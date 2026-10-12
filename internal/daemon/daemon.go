// Package daemon handles background running: detaching, the single-instance
// lock, and finding/stopping the running copy.
//
// The flock on config.LockPath is the source of truth for "is it running": the
// kernel releases it when the process exits, however it exits. The pid file is
// only consulted while that lock is held, so a stale pid can never be signalled.
package daemon

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/apurvtaneja/dictator/internal/config"
)

const logMaxBytes = 2 << 20

// Lock takes the single-instance lock. Keep the returned file open for the
// lifetime of the process; ok is false if another instance holds it.
func Lock() (f *os.File, ok bool, err error) {
	if err := os.MkdirAll(config.Dir, 0o700); err != nil {
		return nil, false, err
	}
	f, err = os.OpenFile(config.LockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}

// RunningPID returns the pid of the live instance, or 0.
func RunningPID() int {
	f, ok, err := Lock()
	if err != nil {
		return 0
	}
	if ok { // we could take it, so nobody holds it
		f.Close()
		return 0
	}
	data, err := os.ReadFile(config.PIDPath)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func WritePID() error {
	return os.WriteFile(config.PIDPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func ClearPID() {
	if data, err := os.ReadFile(config.PIDPath); err == nil &&
		strings.TrimSpace(string(data)) == strconv.Itoa(os.Getpid()) {
		os.Remove(config.PIDPath)
	}
}

// Detach starts this executable again with args, in a new session, with output
// appended to the log. It returns the child's pid.
func Detach(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(config.Dir, 0o700); err != nil {
		return 0, err
	}
	RotateLog()
	log, err := os.OpenFile(config.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer log.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survives the terminal closing
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

// Stop asks the running instance to quit (SIGTERM), escalating to SIGKILL
// after timeout. It returns the pid that was stopped, or 0 if none was running.
func Stop(timeout time.Duration) (int, error) {
	pid := RunningPID()
	if pid == 0 {
		return 0, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return pid, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if RunningPID() == 0 {
			return pid, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(200 * time.Millisecond)
	if RunningPID() != 0 {
		return pid, errors.New("process did not exit")
	}
	return pid, nil
}

// RotateLog keeps dictate.log from growing without bound.
func RotateLog() {
	if st, err := os.Stat(config.LogPath); err == nil && st.Size() > logMaxBytes {
		_ = os.Rename(config.LogPath, config.LogPath+".1")
	}
}
