package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMonitorWithUnavailableRekorServer(t *testing.T) {
	unavailableServer := "http://127.0.0.1:54321"
	binary, checkpointFile, monitorPort := setupTest(t, unavailableServer, true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, unavailableServer)

	waitCh := make(chan error, 1)
	go func() { waitCh <- runCmd.Wait() }()

	// Wait until expected retry/error logs appear
	timeout := time.After(10 * time.Second)
	tick := time.Tick(500 * time.Millisecond)
	found := false
Loop:
	for {
		select {
		case <-timeout:
			break Loop
		case <-tick:
			out := logs.String()
			if strings.Contains(out, "retry cancelled after") ||
				strings.Contains(out, "connection refused") ||
				strings.Contains(out, "dial tcp") {
				found = true
				break Loop
			}
		}
	}

	if !found {
		t.Fatalf("expected retry/cancel logs not found:\n%s", logs.String())
	}

	// Stop the monitor via context cancel
	cancel()

	select {
	case err := <-waitCh:
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
