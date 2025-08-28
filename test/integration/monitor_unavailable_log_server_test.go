package integration

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestMonitorWithUnavailableRekorServer(t *testing.T) {
	unavailableServer := "http://127.0.0.1:54321"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checkpointFile := createCheckpointFile(ctx, t, unavailableServer, false)
	monitorPort, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	var runCmd *exec.Cmd
	t.Run("start_monitor", func(t *testing.T) {
		runCmd = startMonitorCommand(ctx, checkpointFile, monitorPort, unavailableServer)
	})

	var outBuf bytes.Buffer
	runCmd.Stderr = &outBuf
	err = runCmd.Run()
	if err == nil {
		t.Fatalf("monitor is expected to faile")
	}

	if out := outBuf.String(); !strings.Contains(out, "retry cancelled after") {
		t.Fatalf("expected \"retry cancelled after\" logs not found:\n%s", outBuf.String())
	}

}
