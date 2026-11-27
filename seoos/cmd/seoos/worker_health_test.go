package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerHealthRequiresFreshHeartbeatAndLiveProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat")
	if workerHealth(path, time.Now()) == nil {
		t.Fatal("missing heartbeat was accepted")
	}
	if err := writeWorkerHeartbeat(path); err != nil {
		t.Fatal(err)
	}
	if err := workerHealth(path, time.Now()); err != nil {
		t.Fatal(err)
	}
	if workerHealth(path, time.Now().Add(workerHeartbeatMaxAge+time.Second)) == nil {
		t.Fatal("stale heartbeat was accepted")
	}
	if err := os.WriteFile(path, []byte("not-a-pid"), 0600); err != nil {
		t.Fatal(err)
	}
	if workerHealth(path, time.Now()) == nil {
		t.Fatal("invalid process was accepted")
	}
}
