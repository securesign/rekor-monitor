package integration

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// truncateAndForkCheckpointFile reads a checkpoint file, truncates its tree size, forks its root hash,
// and writes the modified content back to the file.
//
// The function performs the following steps:
//  1. Reads the checkpoint file content.
//  2. Preserves trailing newline and escaped newline ("\\n") if present.
//  3. Splits the content into lines using "\\n" as the delimiter, expecting at least 3 lines
//     (based on the checkpoint file format: tree ID, tree size, root hash, and optional signature).
//     The SplitN function uses 4 as the maximum number of splits to ensure the signature (if present)
//     is captured as a single line, even if it contains additional "\\n" separators.
//  4. Truncates the tree size (line 2) by reducing it by 10 if it's greater than 10, or setting it to 1 otherwise,
//     to simulate a log truncation.
//  5. Forks the root hash (line 3) by appending "forked" to simulate a log fork.
//  6. Reconstructs the content with the original trailing newline and escaped newline properties.
//  7. Writes the modified content back to the file with 0644 permissions.
//
// The number 4 in SplitN is chosen to accommodate the expected checkpoint file format:
//   - Line 1: Tree ID
//   - Line 2: Tree size
//   - Line 3: Root hash
//   - Line 4+: Optional signature or additional data
//
// By limiting to 4 splits, we ensure the signature (which may contain "\\n") is not split further.
// The number 10 for tree size reduction is chosen to simulate a significant truncation while
// ensuring the resulting tree size remains positive.
func truncateAndForkCheckpointFile(t *testing.T, checkpointFile string) {
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
