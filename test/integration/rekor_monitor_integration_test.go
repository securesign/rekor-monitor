package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type MonitorExpectations struct {
	ExpectErrorLog       bool
	ExpectedFailureCount int
	ExpectedTotalCount   int
}

func TestMonitorWithValidCheckpoint(t *testing.T) {
	ctx, binary, checkpointFile, monitorPort, mockServer := setupTest(t)
	defer mockServer.Close()

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, mockServer)

	metrics := fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   1,
	})

	time.Sleep(1 * time.Second)

	metrics = fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   2,
	})

	stopMonitor(t, runCmd)
}

func TestTamperedCheckpoint(t *testing.T) {
	ctx, binary, checkpointFile, monitorPort, mockServer := setupTest(t)
	defer mockServer.Close()

	t.Run("validate_and_tamper_checkpoint_file", func(t *testing.T) {
		content, err := os.ReadFile(checkpointFile)
		if err != nil {
			t.Fatalf("failed to read checkpoint file: %v", err)
		}
		contentStr := string(content)
		wasNewlineTrailing := strings.HasSuffix(contentStr, "\n")
		contentStr = strings.TrimSuffix(contentStr, "\n")
		wasEscapedTrailing := strings.HasSuffix(contentStr, "\\n")
		contentStr = strings.TrimSuffix(contentStr, "\\n")
		lines := strings.SplitN(contentStr, "\\n", 4)
		if len(lines) < 3 {
			t.Fatalf("invalid checkpoint file format: expected at least 3 lines, got %d", len(lines))
		}
		lines[2] = lines[2] + "tampered"
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
	})

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, mockServer)

	metrics := fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       true,
		ExpectedFailureCount: 1,
		ExpectedTotalCount:   1,
	})

	stopMonitor(t, runCmd)
}

func TestLogTruncationForking(t *testing.T) {
	ctx, binary, checkpointFile, monitorPort, mockServer := setupTest(t)
	defer mockServer.Close()

	t.Run("truncate_fork_checkpoint_file", func(t *testing.T) {
		content, err := os.ReadFile(checkpointFile)
		if err != nil {
			t.Fatalf("failed to read checkpoint file: %v", err)
		}

		contentStr := string(content)
		wasNewlineTrailing := strings.HasSuffix(contentStr, "\n")
		contentStr = strings.TrimSuffix(contentStr, "\n")
		wasEscapedTrailing := strings.HasSuffix(contentStr, "\\n")
		contentStr = strings.TrimSuffix(contentStr, "\\n")

		lines := strings.SplitN(contentStr, "\\n", 4)
		if len(lines) < 3 {
			t.Fatalf("invalid checkpoint file format: expected at least 3 lines, got %d", len(lines))
		}

		treeSizeLine := lines[1]
		rootHashLine := lines[2]

		// Truncate: decrease the tree size
		newTreeSize := "0"
		if n, err := strconv.Atoi(treeSizeLine); err == nil && n > 10 {
			newTreeSize = fmt.Sprintf("%d", n-10)
		} else if err == nil {
			newTreeSize = "1"
		}
		// Fork: alter the root hash
		newRootHash := rootHashLine + "forked"

		lines[1] = newTreeSize
		lines[2] = newRootHash

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
	})

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, mockServer)

	metrics := fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       true,
		ExpectedFailureCount: 1,
		ExpectedTotalCount:   1,
	})

	stopMonitor(t, runCmd)
}

// setupTest prepares the test environment: builds the binary, initializes the checkpoint file,
// and allocates a port. Returns the context, binary path, checkpoint file path, and monitor port.
func setupTest(t *testing.T) (context.Context, string, string, string, *httptest.Server) {
	t.Helper()

	mockServer := StartMockRekorServer()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	buildDir := t.TempDir()
	binary := filepath.Join(buildDir, "rekor_monitor")
	buildCmd := exec.Command("go", "build", "-o", binary, "../../cmd/rekor_monitor/main.go")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("failed to build binary: %v", err)
	}

	dataDir := t.TempDir()
	checkpointFile := filepath.Join(dataDir, "checkpoint_log.txt")

	t.Run("generate_initial_checkpoint_file", func(t *testing.T) {
		initCmd := exec.CommandContext(ctx, binary,
			"--once",
			"--file", checkpointFile,
			"--url", mockServer.URL,
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
	})

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	monitorPort := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	return ctx, binary, checkpointFile, monitorPort, mockServer
}

// Start the monitor and return the Cmd and logs builder
func startMonitor(t *testing.T, ctx context.Context, binary, checkpointFile, monitorPort string, server *httptest.Server) (*exec.Cmd, *strings.Builder) {
	t.Helper()

	var runCmd *exec.Cmd
	var logs *strings.Builder

	t.Run("start_monitor", func(t *testing.T) {
		runCmd = exec.CommandContext(ctx, binary,
			"--once=false",
			"--interval=2s",
			"--file", checkpointFile,
			"--url", server.URL,
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

		logs = &strings.Builder{}
		go io.Copy(logs, stdoutPipe)
		go io.Copy(logs, stderrPipe)

		// give monitor time to start
		time.Sleep(1500 * time.Millisecond)
	})

	return runCmd, logs
}

// Fetch metrics from the monitor with retry
func fetchMetrics(t *testing.T, monitorPort string) string {
	t.Helper()

	var metricsStr string

	t.Run("fetch_metrics", func(t *testing.T) {
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
			} else {
				t.Logf("failed to fetch metrics: %v", err)
			}
			time.Sleep(500 * time.Millisecond)
		}

		if metricsStr == "" {
			t.Fatalf("failed to query metrics after retries")
		}
	})

	return metricsStr
}

// Validate logs and metrics against expectations
func validateLogsAndMetrics(t *testing.T, logs *strings.Builder, metricsStr string, exp MonitorExpectations) {
	t.Helper()

	t.Run("validate_logs_and_metrics", func(t *testing.T) {
		if exp.ExpectErrorLog {
			if !strings.Contains(logs.String(), "error running consistency check") {
				t.Errorf("expected error log not found:\n%s", logs.String())
			}
		} else {
			if strings.Contains(logs.String(), "error") {
				t.Errorf("unexpected error found in logs:\n%s", logs.String())
			}
		}

		failMetric := fmt.Sprintf("log_index_verification_failure %d", exp.ExpectedFailureCount)
		if !strings.Contains(metricsStr, failMetric) {
			t.Errorf("expected failure metric '%s' not found:\n%s", failMetric, metricsStr)
		}

		totalMetric := fmt.Sprintf("log_index_verification_total %d", exp.ExpectedTotalCount)
		if !strings.Contains(metricsStr, totalMetric) {
			t.Errorf("expected total metric '%s' not found:\n%s", totalMetric, metricsStr)
		}
	})
}

// Stop the monitor
func stopMonitor(t *testing.T, runCmd *exec.Cmd) {
	t.Helper()

	t.Run("stop_monitor", func(t *testing.T) {
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
	})
}

// StartMockRekorServer returns an httptest.Server that mimics Rekor endpoints.
func StartMockRekorServer() *httptest.Server {
	handler := http.NewServeMux()

	handler.HandleFunc("/api/v1/log/publicKey", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		fmt.Fprint(w, `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE2G2Y+2tabdTV5BcGiBIx0a9fAFwr
kBbmLSGtks4L3qX6yYY0zufBnhC8Ur/iy55GhWP/9A/bY2LhC30M9+RYtw==
-----END PUBLIC KEY-----`)
	})

	handler.HandleFunc("/api/v1/log", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
  "rootHash": "dd984b288de629496979d43d86220c6c92232abdef4dcfae7958b2c56ab04060",
  "signedTreeHead": "rekor.sigstore.dev - 1193050959916656506\n266676745\n3ZhLKI3mKUlpedQ9hiIMbJIjKr3vTc+ueViyxWqwQGA=\n\n— rekor.sigstore.dev wNI9ajBEAiAE7ER4yd8Waq4ZzLQt9BIUyfAvbizhv5PCcxk5Glf28AIgWT6LH4VlkrI8VhZnqCPigxEVzdlwVQpuRo0OsISdPGs=\n",
  "treeID": "1193050959916656506",
  "treeSize": 266676745
}`)
	})

	return httptest.NewServer(handler)
}
