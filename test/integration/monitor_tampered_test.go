package integration

import (
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
	ctx, binary, checkpointFile, monitorPort, mockServer := setupTest(t)
	defer mockServer.Close()

	t.Run("validate_and_tamper_checkpoint_file", func(t *testing.T) {
		tamperCheckpointRootHash(t, checkpointFile)
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
