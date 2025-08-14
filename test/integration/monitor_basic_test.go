package integration

import (
	"testing"
	"time"
)

func TestMonitorWithValidCheckpoint(t *testing.T) {
	mockServer := RekorServer().WithData().Build()
	ctx, binary, checkpointFile, monitorPort := setupTest(t, mockServer.URL, false)
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

	stopMonitor(t, runCmd)
}

func TestMonitorWithEmptyLog(t *testing.T) {
	emptyMockServer := RekorServer().Build()
	ctx, binary, checkpointFile, monitorPort := setupTest(t, emptyMockServer.URL, false)
	defer emptyMockServer.Close()

	runCmd, logs := startMonitor(t, ctx, binary, checkpointFile, monitorPort, emptyMockServer.URL)

	metrics := fetchMetrics(t, monitorPort)
	validateLogsAndMetrics(t, logs, metrics, MonitorExpectations{
		ExpectErrorLog:       false,
		ExpectedFailureCount: 0,
		ExpectedTotalCount:   1,
	})

	stopMonitor(t, runCmd)
}
