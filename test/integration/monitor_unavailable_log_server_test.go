package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMonitorWithUnavailableRekorServer(t *testing.T) {
	unavailableServer := "http://127.0.0.1:54321"
	ctx, binary, checkpointFile, monitorPort := setupTest(t, unavailableServer, true)

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, unavailableServer)

	waitCh := make(chan error, 1)
	go func() { waitCh <- runCmd.Wait() }()

	require.Eventually(t, func() bool {
		out := logs.String()
		return strings.Contains(out, "retry cancelled after") ||
			strings.Contains(out, "connection refused") ||
			strings.Contains(out, "dial tcp")
	}, 10*time.Second, 500*time.Millisecond, "expected retry/cancel logs not found")

	// Context from setupTest is cleaned up automatically
	select {
	case err := <-waitCh:
		fmt.Println("err.Error() ", err.Error())
		if err == nil {
			t.Fatal("expected monitor to exit with error, got nil")
		}
		if !strings.Contains(err.Error(), "exit status 1") {
			t.Fatalf("unexpected exit error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not exit after context cancellation")
	}
}
