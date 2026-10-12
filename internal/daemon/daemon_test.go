package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apurvtaneja/dictator/internal/config"
)

func TestLockAndRunningPID(t *testing.T) {
	dir := t.TempDir()
	config.Dir = dir
	config.LockPath = filepath.Join(dir, "x.lock")
	config.PIDPath = filepath.Join(dir, "x.pid")

	if RunningPID() != 0 {
		t.Fatal("nothing should be running")
	}
	f, ok, err := Lock()
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	WritePID()
	// a second open file description can't take the lock
	if _, ok2, _ := Lock(); ok2 {
		t.Fatal("second lock succeeded")
	}
	if RunningPID() != os.Getpid() {
		t.Fatalf("RunningPID = %d", RunningPID())
	}
	ClearPID()
	f.Close()
	if RunningPID() != 0 {
		t.Fatal("lock not released")
	}
}
