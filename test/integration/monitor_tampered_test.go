package integration

import (
	"os"
	"strings"
	"testing"
)

// tamperCheckpointRootHash reads a checkpoint file, tampers with its root hash,
// and writes the modified content back to the file.
//
// The function performs the following steps:
//  1. Reads the checkpoint file content.
//  2. Preserves trailing newline and escaped newline ("\\n") if present.
//  3. Splits the content into lines using "\\n" as the delimiter, expecting at least 3 lines
//     (based on the checkpoint file format: tree ID, tree size, root hash, and optional signature).
//     The SplitN function uses 4 as the maximum number of splits to ensure the signature (if present)
//     is captured as a single line, even if it contains additional "\\n" separators.
//  4. Modifies the root hash (line 3) by appending "tampered" to simulate a corrupted checkpoint.
//  5. Reconstructs the content with the original trailing newline and escaped newline properties.
//  6. Writes the tampered content back to the file with 0644 permissions.
//
// The number 4 in SplitN is chosen to accommodate the expected checkpoint file format:
//   - Line 1: Tree ID
//   - Line 2: Tree size
//   - Line 3: Root hash
//   - Line 4+: Optional signature or additional data
//
// By limiting to 4 splits, we ensure the signature (which may contain "\\n") is not split further.
func tamperCheckpointRootHash(t *testing.T, checkpointFile string) {
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
