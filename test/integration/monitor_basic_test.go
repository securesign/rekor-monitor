package integration

import (
	"context"
	"testing"
	"time"
)

func TestMonitorWithValidCheckpoint(t *testing.T) {
	mockServer := RekorServer().WithData().Build()
	binary, checkpointFile, monitorPort := setupTest(t, mockServer.URL, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer mockServer.Close()

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, mockServer.URL)

	metrics := fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   1,
	})

	time.Sleep(1 * time.Second)

	metrics = fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   2,
	})

	stopMonitor(t, cancel, runCmd)
}

func TestMonitorWithEmptyLog(t *testing.T) {
	emptyMockServer := RekorServer().Build()
	binary, checkpointFile, monitorPort := setupTest(t, emptyMockServer.URL, false)
	defer emptyMockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, emptyMockServer.URL)

	metrics := fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   1,
	})

	stopMonitor(t, cancel, runCmd)
}
