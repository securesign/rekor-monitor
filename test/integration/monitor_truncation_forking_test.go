package integration

import (
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
	ctx, binary, checkpointFile, monitorPort, mockServer := setupTest(t)
	defer mockServer.Close()

	t.Run("truncate_fork_checkpoint_file", func(t *testing.T) {
		truncateAndForkCheckpointFile(t, checkpointFile)
	})

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, mockServer)

	metrics := fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       true,
		ExpectedErrorType:    "error running consistency check",
		ExpectedFailureCount: 1,
		ExpectedTotalCount:   1,
	})

	stopMonitor(t, runCmd)
}
