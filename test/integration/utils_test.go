package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type MonitorExpectations struct {
	ExpectErrorLog       bool
	ExpectedErrorType    string
	ExpectedFailureCount int
	ExpectedTotalCount   int
}

// RekorServerBuilder helps construct mock Rekor servers with different states.
type RekorServerBuilder struct {
	publicKey string
	logJSON   string
}

// RekorServer returns a new builder preconfigured with an empty log.
func RekorServer() *RekorServerBuilder {
	return &RekorServerBuilder{
		publicKey: `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEFSHl2cMn87xLeZuOo0q3tGgdj8+y
x1SXoyVJNLAXZiYXCdPm7+DULIZXyKSSv6RS2emHrBbWtCrQtBaM3GxlMA==
-----END PUBLIC KEY-----`,
		logJSON: `{
  "rootHash": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "signedTreeHead": "c45c80833111 - 2882947332475159079\n0\n47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=\n\n— c45c80833111 8YHtBzBFAiBOzlkS1nQNcmgd24f/gawQ/LRYyUh6NjO55Pn3PJTbZgIhAPyb+DCWdgFNqQVmVp8eBaSTrCwdICr09QMiNtyPgvGm\n",
  "treeID": "2882947332475159079",
  "treeSize": 0
}`,
	}
}

// setupTest prepares the test environment: builds the binary, initializes the checkpoint file,
// and allocates a port. Returns the context, binary path, checkpoint file path, and monitor port.
//
// Some tests (for example, when the Rekor server is intentionally unreachable) cannot generate
// a checkpoint. In those cases, pass skipCheckpoint = true to skip this step.
func setupTest(t *testing.T, serverUrl string, skipCheckpoint bool) (string, string, string) {
	t.Helper()

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

	if !skipCheckpoint {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Only run initial checkpoint if server is reachable
		t.Run("generate_initial_checkpoint_file", func(t *testing.T) {
			initCmd := exec.CommandContext(ctx, binary,
				"--once",
				"--file", checkpointFile,
				"--url", serverUrl,
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
	}
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	monitorPort := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	return binary, checkpointFile, monitorPort
}

// Start the monitor and return the Cmd and logs builder
func startMonitor(t *testing.T, ctx context.Context, binary, checkpointFile, monitorPort string, serverUrl string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()

	var runCmd *exec.Cmd
	var logsBuf bytes.Buffer

	t.Run("start_monitor", func(t *testing.T) {
		runCmd = exec.CommandContext(ctx, binary,
			"--once=false",
			"--interval=2s",
			"--file", checkpointFile,
			"--url", serverUrl,
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

		// Merge stdout + stderr into one buffer
		mw := io.MultiWriter(&logsBuf)
		go io.Copy(mw, stdoutPipe)
		go io.Copy(mw, stderrPipe)

		// give monitor time to start
		time.Sleep(1500 * time.Millisecond)
	})

	return runCmd, &logsBuf
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
func validateLogsAndMetrics(t *testing.T, logs *bytes.Buffer, metricsStr string, exp MonitorExpectations) {
	t.Helper()

	t.Run("validate_logs_and_metrics", func(t *testing.T) {
		if exp.ExpectErrorLog {
			if !strings.Contains(logs.String(), "error running consistency check") {
				t.Errorf("expected error log not found:\n%s", logs.String())
			}
			if exp.ExpectedErrorType != "" && !strings.Contains(logs.String(), exp.ExpectedErrorType) {
				t.Errorf("expected error type '%s' not found in logs:\n%s", exp.ExpectedErrorType, logs.String())
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
func stopMonitor(t *testing.T, cancel context.CancelFunc, runCmd *exec.Cmd) {
	t.Helper()

	t.Run("stop_monitor", func(t *testing.T) {
		cancel()

		select {
		case <-time.After(5 * time.Second):
			t.Fatalf("monitor did not exit within 5 seconds after SIGTERM")
		case err := <-func() chan error {
			errChan := make(chan error, 1)
			go func() {
				errChan <- runCmd.Wait()
			}()
			return errChan
		}():
			if err != nil && !strings.Contains(err.Error(), "killed") {
				t.Fatalf("monitor did not exit cleanly: %v", err)
			}
		}
	})
}

// WithData configures the server to serve a non-empty log.
func (b *RekorServerBuilder) WithData() *RekorServerBuilder {
	b.publicKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE2G2Y+2tabdTV5BcGiBIx0a9fAFwr
kBbmLSGtks4L3qX6yYY0zufBnhC8Ur/iy55GhWP/9A/bY2LhC30M9+RYtw==
-----END PUBLIC KEY-----`

	b.logJSON = `{
  "rootHash": "dd984b288de629496979d43d86220c6c92232abdef4dcfae7958b2c56ab04060",
  "signedTreeHead": "rekor.sigstore.dev - 1193050959916656506\n266676745\n3ZhLKI3mKUlpedQ9hiIMbJIjKr3vTc+ueViyxWqwQGA=\n\n— rekor.sigstore.dev wNI9ajBEAiAE7ER4yd8Waq4ZzLQt9BIUyfAvbizhv5PCcxk5Glf28AIgWT6LH4VlkrI8VhZnqCPigxEVzdlwVQpuRo0OsISdPGs=\n",
  "treeID": "1193050959916656506",
  "treeSize": 266676745
}`
	return b
}

// Build spins up the httptest.Server with the chosen configuration.
func (b *RekorServerBuilder) Build() *httptest.Server {
	handler := http.NewServeMux()

	handler.HandleFunc("/api/v1/log/publicKey", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		fmt.Fprint(w, b.publicKey)
	})

	handler.HandleFunc("/api/v1/log", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, b.logJSON)
	})

	return httptest.NewServer(handler)
}

// modifyCheckpointFile reads a checkpoint file, applies modifications via a callback function,
// and writes the modified content back to the file.
//
// Parameters:
//   - t: The testing object for error reporting.
//   - checkpointFile: The path to the checkpoint file to modify.
//   - modify: A callback function that applies changes to the parsed lines.
//
// The function performs the following steps:
//  1. Reads the checkpoint file content.
//  2. Preserves trailing newline and escaped newline ("\\n") if present.
//  3. Splits the content into lines using "\\n" as the delimiter, expecting at least 3 lines
//     (based on the checkpoint file format: tree ID, tree size, root hash, and optional signature).
//     The SplitN function uses 4 as the maximum number of splits to ensure the signature (if present)
//     is captured as a single line, even if it contains additional "\\n" separators.
//  4. Calls the provided callback function to modify the lines (e.g., altering the tree size or root hash).
//  5. Reconstructs the content with the original trailing newline and escaped newline properties.
//  6. Writes the modified content back to the file with 0644 permissions.
//
// The number 4 in SplitN is chosen to accommodate the expected checkpoint file format:
//   - Line 1: Tree ID
//   - Line 2: Tree size
//   - Line 3: Root hash
//   - Line 4+: Optional signature or additional data
//
// By limiting to 4 splits, we ensure the signature (which may contain "\\n") is not split further.
func modifyCheckpointFile(t *testing.T, checkpointFile string, modify func(lines []string)) {
	t.Helper()

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

	modify(lines)

	modifiedContent := strings.Join(lines, "\\n")
	if wasEscapedTrailing {
		modifiedContent += "\\n"
	}

	modifiedBytes := []byte(modifiedContent)
	if wasNewlineTrailing {
		modifiedBytes = append(modifiedBytes, '\n')
	}

	if err := os.WriteFile(checkpointFile, modifiedBytes, 0644); err != nil {
		t.Fatalf("failed to write modified checkpoint: %v", err)
	}
}
