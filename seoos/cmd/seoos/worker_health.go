package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The longest supported job has a 130-second deadline. A blocked claim or
// worker that exceeds the longest task window cannot refresh this heartbeat.
const workerHeartbeatMaxAge = 150 * time.Second

func workerHeartbeatPath() string {
	if path := os.Getenv("SEOOS_WORKER_HEARTBEAT_FILE"); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "seoos-worker-heartbeat")
}

func heartbeatWriter(path string) func() {
	var last time.Time
	return func() {
		now := time.Now()
		if !last.IsZero() && now.Sub(last) < 5*time.Second {
			return
		}
		last = now
		if err := writeWorkerHeartbeat(path); err != nil {
			slog.Warn("SEO worker heartbeat is unavailable")
		}
	}
}

func writeWorkerHeartbeat(path string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".seoos-worker-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func workerHealth(path string, now time.Time) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || now.Before(info.ModTime()) || now.Sub(info.ModTime()) > workerHeartbeatMaxAge {
		return errors.New("SEO worker heartbeat is missing or stale")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.New("SEO worker heartbeat cannot be read")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || syscall.Kill(pid, 0) != nil {
		return errors.New("SEO worker process is unavailable")
	}
	return nil
}
