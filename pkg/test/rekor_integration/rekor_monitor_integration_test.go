// Copyright 2024 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTamperedCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	buildDir := t.TempDir()
	binary := filepath.Join(buildDir, "rekor_monitor")
	buildCmd := exec.Command("go", "build", "-o", binary, "github.com/sigstore/rekor-monitor/cmd/rekor_monitor")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("failed to build binary: %v", err)
	}

	dataDir := t.TempDir()
	checkpointFile := filepath.Join(dataDir, "checkpoint_log.txt")

	// First run to generate initial checkpoint file
	initCmd := exec.CommandContext(ctx, binary,
		"--once",
		"--file", checkpointFile,
		"--url", "https://rekor.sigstore.dev",
	)
	initCmd.Stdout = os.Stdout
	initCmd.Stderr = os.Stderr
	err := initCmd.Run()
	if err == nil {
		t.Fatal("expected error on initial run due to no start index")
	}

	if _, err := os.Stat(checkpointFile); err != nil {
		t.Fatalf("checkpoint file not created: %v", err)
	}

	// Tamper with the root hash in the checkpoint file
	content, err := os.ReadFile(checkpointFile)
	if err != nil {
		t.Fatalf("failed to read checkpoint file: %v", err)
	}
	contentStr := string(content)
	wasNewlineTrailing := strings.HasSuffix(contentStr, "\n")
	contentStr = strings.TrimSuffix(contentStr, "\n")
	wasEscapedTrailing := strings.HasSuffix(contentStr, "\\n")
	contentStr = strings.TrimSuffix(contentStr, "\\n")
	// Split on \\n to get log name, tree size, root hash, and signature
	lines := strings.SplitN(contentStr, "\\n", 4)
	if len(lines) < 3 {
		t.Fatalf("invalid checkpoint file format: expected at least 3 lines, got %d", len(lines))
	}
	lines[2] = lines[2] + "tampered" // Tamper with root hash
	tamperedContent := strings.Join(lines, "\\n")
	if wasEscapedTrailing {
		tamperedContent += "\\n"
	}
	tamperedBytes := []byte(tamperedContent)
	if wasNewlineTrailing {
		tamperedBytes = append(tamperedBytes, '\n')
	}
	if err := os.WriteFile(checkpointFile, tamperedBytes, 0644); err != nil {
		t.Fatalf("failed to write tampered checkpoint: %v", err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	monitorPort := fmt.Sprintf("%d", port)

	// Run the monitor
	runCmd := exec.CommandContext(ctx, binary,
		"--once=false",
		"--interval=2s",
		"--file", checkpointFile,
		"--url", "https://rekor.sigstore.dev",
		"--monitor-port", monitorPort,
	)
	stdoutPipe, err := runCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to get stdout pipe: %v", err)
	}
	stderrPipe, err := runCmd.StderrPipe()
	if err != nil {
		t.Fatalf("failed to get stderr pipe: %v", err)
	}
	if err := runCmd.Start(); err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}
	defer runCmd.Process.Kill()

	var logs strings.Builder
	go io.Copy(&logs, stdoutPipe)
	go io.Copy(&logs, stderrPipe)

	// Wait for the first iteration to complete
	time.Sleep(1500 * time.Millisecond)

	var metricsStr string
	for i := 0; i < 5; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%s/metrics", monitorPort))
		if err == nil {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				metricsStr = string(body)
				break
			}
			t.Logf("failed to read metrics body: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if metricsStr == "" {
		t.Fatalf("failed to query metrics after retries")
	}

	expectedError := "error running consistency check"
	if !strings.Contains(logs.String(), expectedError) {
		t.Errorf("expected error '%s' not found in logs: %s", expectedError, logs.String())
	}

	expectedMetric := "log_index_verification_failure 1"
	if !strings.Contains(metricsStr, expectedMetric) {
		t.Errorf("expected metric '%s' not found: %s", expectedMetric, metricsStr)
	}

	if err := runCmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send SIGTERM to monitor: %v", err)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Fatalf("monitor did not exit within 5 seconds after SIGTERM")
	case err := <-func() chan error {
		errChan := make(chan error)
		go func() {
			errChan <- runCmd.Wait()
		}()
		return errChan
	}():
		if err != nil && !strings.Contains(err.Error(), "signal: terminated") {
			t.Fatalf("monitor did not exit cleanly: %v", err)
		}
	}
}
