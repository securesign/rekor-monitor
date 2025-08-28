package integration

import (
	"bytes"
	"context"
	"testing"
)

// tamperCheckpointRootHash modifies a checkpoint file by appending "tampered" to its root hash
// to simulate a corrupted checkpoint.
//
// It uses modifyCheckpointFile to handle the file operations, passing a function that
// changes the root hash (third line) of the checkpoint.
func tamperCheckpointRootHash(t *testing.T, checkpointFile string) {
	t.Helper()

	modifyCheckpointFile(t, checkpointFile, func(lines []string) {
		lines[2] = lines[2] + "tampered"
	})
}

func TestTamperedCheckpoint(t *testing.T) {
	mockServer := RekorServer().WithData().Build()
	defer mockServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	checkpointFile := createCheckpointFile(ctx, t, mockServer.URL, true)
	t.Run("validate_and_tamper_checkpoint_file", func(t *testing.T) {
		tamperCheckpointRootHash(t, checkpointFile)
	})

	monitorPort, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	runCmd := startMonitorCommand(ctx, checkpointFile, monitorPort, mockServer.URL)
	logs := bytes.NewBuffer(nil)
	runCmd.Stderr = logs
	if err := runCmd.Start(); err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	metrics, err := fetchMetrics(monitorPort)
	if err != nil {
		t.Fatalf("failed to fetch metrics: %v", err)
	}
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       true,
		ExpectedErrorType:    "error running consistency check",
		ExpectedFailureCount: 1,
		ExpectedTotalCount:   1,
	})

	cancel()
	// Wait for the monitor to exit, test timeouts if it doesn't
	runCmd.Wait()
}
