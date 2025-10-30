package integration

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"testing"
)

// truncateAndForkCheckpointFile modifies a checkpoint file by reducing its tree size and
// appending "forked" to its root hash to simulate a log truncation and fork.
//
// It uses modifyCheckpointFile to handle the file operations, passing a function that:
// - Reduces the tree size (second line) by 10 if it's greater than 10, or sets it to 1 otherwise.
// - Appends "forked" to the root hash (third line).
func truncateAndForkCheckpointFile(t *testing.T, checkpointFile string) {
	t.Helper()

	modifyCheckpointFile(t, checkpointFile, func(lines []string) {
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
	})
}

func TestLogTruncationForking(t *testing.T) {
	mockServer := RekorServer().WithData().Build()
	defer mockServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	checkpointFile := createCheckpointFile(ctx, t, mockServer.URL, true)

	t.Run("truncate_fork_checkpoint_file", func(t *testing.T) {
		truncateAndForkCheckpointFile(t, checkpointFile)
	})

	monitorPort, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	runCmd := startMonitorCommand(ctx, checkpointFile, monitorPort, mockServer.URL, defaultInterval)
	logs := bytes.NewBuffer(nil)
	runCmd.Stderr = logs
	if err := runCmd.Start(); err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	metrics, err := fetchMetrics(monitorPort)
	if err != nil {
		t.Logf("rekor-monitor logs:\n%s", logs.String())
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
