package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
