package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

var binaryPath string

func TestMain(m *testing.M) {
	if err := buildMonitorCmd(); err != nil {
		log.Fatal("Unable to build rekor-monitor cli %w", err)
		os.Exit(1)
	}

	exitVal := m.Run()

	os.Exit(exitVal)
}

func buildMonitorCmd() error {
	buildDir := os.TempDir()
	binaryPath = filepath.Join(buildDir, "rekor_monitor")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/rekor_monitor/main.go")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	return buildCmd.Run()
}

func createCheckpointFile(ctx context.Context, t *testing.T, serverUrl string, initialize bool) string {

	dataDir := t.TempDir()
	checkpointFile := filepath.Join(dataDir, "checkpoint_log.txt")

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if !initialize {
		return checkpointFile
	}

	// Only run initial checkpoint if server is reachable
	t.Run("generate_initial_checkpoint_file", func(t *testing.T) {
		initCmd := exec.CommandContext(ctx, binaryPath,
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

	return checkpointFile
}

func findFreePort() (int, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port, nil
}

// Start the monitor and return the Cmd and logs builder
func startMonitorCommand(ctx context.Context, checkpointFile string, monitorPort int, serverUrl string) *exec.Cmd {
	return exec.CommandContext(ctx, binaryPath,
		"--once=false",
		"--interval=2s",
		"--file", checkpointFile,
		"--url", serverUrl,
		"--monitor-port", fmt.Sprintf("%d", monitorPort),
	)

}

// Fetch metrics from the monitor with retry
func fetchMetrics(monitorPort int) (string, error) {
	clientBuilder := retryablehttp.NewClient()
	clientBuilder.RetryMax = 5
	clientBuilder.RetryWaitMin = 1 * time.Second
	clientBuilder.RetryWaitMax = 5 * time.Second
	clientBuilder.Backoff = retryablehttp.DefaultBackoff

	client := clientBuilder.StandardClient()

	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/metrics", monitorPort))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)

	return string(body), nil
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
