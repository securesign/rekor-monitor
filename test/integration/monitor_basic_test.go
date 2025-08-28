package integration

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestMonitorWithValidCheckpoint(t *testing.T) {
	mockServer := RekorServer().WithData().Build()
	defer mockServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	checkpointFile := createCheckpointFile(ctx, t, mockServer.URL, false)
	monitorPort, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	runCmd := startMonitorCommand(ctx, checkpointFile, monitorPort, mockServer.URL)
	logs := bytes.NewBuffer(nil)
	runCmd.Stdout = logs
	runCmd.Stderr = logs
	if err := runCmd.Start(); err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	metrics, err := fetchMetrics(monitorPort)
	if err != nil {
		t.Fatalf("failed to fetch metrics: %v", err)
	}

	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   1,
	})
	// wait for second checkpoint to be written
	time.Sleep(2 * time.Second)

	metrics, err = fetchMetrics(monitorPort)
	if err != nil {
		t.Fatalf("failed to fetch metrics: %v", err)
	}
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   2,
	})

	cancel()
	// Wait for the monitor to exit, test timeouts if it doesn't
	runCmd.Wait()
}

func TestMonitorWithEmptyLog(t *testing.T) {
	emptyMockServer := RekorServer().Build()
	defer emptyMockServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	checkpointFile := createCheckpointFile(ctx, t, emptyMockServer.URL, false)
	monitorPort, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	runCmd := startMonitorCommand(ctx, checkpointFile, monitorPort, emptyMockServer.URL)
	logs := bytes.NewBuffer(nil)
	runCmd.Stdout = logs
	runCmd.Stderr = logs
	if err := runCmd.Start(); err != nil {
		t.Fatalf("failed to start monitor: %v", err)
	}

	metrics, err := fetchMetrics(monitorPort)
	if err != nil {
		t.Fatalf("failed to fetch metrics: %v", err)
	}
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   1,
	})

	cancel()
	// Wait for the monitor to exit, test timeouts if it doesn't
	runCmd.Wait()
}
