from prometheus_client import start_http_server, Counter
import subprocess
import os
import time
import logging

# Define Prometheus metrics
rekor_check_total = Counter(
    "rekor_consistency_check_total",
    "Rekor checkpoint consistency check result",
    ["status"]
)

# Configure logging
log_level = os.environ.get("LOG_LEVEL", "INFO").upper()
logging.basicConfig(
    level=getattr(logging, log_level, logging.INFO),
    format='%(asctime)s [%(levelname)s] %(message)s',
)
logger = logging.getLogger(__name__)

# Start metrics HTTP server on configurable port (default 9464)
metrics_port = int(os.getenv("metrics_port", "9464"))
start_http_server(metrics_port)

# Check interval (default: 5s)
check_interval = int(os.environ.get("CHECK_INTERVAL_SECONDS", 5))

# Command args
checkpoint_dir = os.environ.get("CHECKPOINT_DIR", "/data")
checkpoint_path = os.path.join(checkpoint_dir, "checkpoint_log.txt")
rekor_server_endpoint = os.environ.get("REKOR_SERVER_ENDPOINT", "https://rekor.sigstore.dev")

def run_rekor_monitor():
    try:
        result = subprocess.run(
            [
                "./rekor_monitor",
                f"--file={checkpoint_path}",
                "--once=true",
                f"--url={rekor_server_endpoint}"
            ],
            capture_output=True,
            text=True
        )
        stderr_lower = result.stderr.lower()

        # Case 1: Consistency verified successfully
        if result.returncode == 0 and "consistency verified" in stderr_lower:
            rekor_check_total.labels(status="success").inc()
            logger.info("Rekor consistency check: SUCCESS")

        # Case 2: Special condition — log is empty and cannot verify consistency
        # This is not considered a failure, but should be logged for visibility
        elif result.returncode == 0 and "consistency proofs can not be computed starting from an empty log" in stderr_lower:
            logger.info("Rekor consistency check skipped: log is empty (not an error)")

        # Case 3: First-run, no checkpoint yet — not a failure
        elif "no start index set and no log checkpoint" in stderr_lower:
            logger.info("Rekor consistency check skipped: no checkpoint found (first run)")

        # Case 4: All other outcomes are treated as failures
        else:
            rekor_check_total.labels(status="failure").inc()
            logger.error("Rekor consistency check: FAILURE")
            logger.error(result.stderr)
    except Exception as e:
        rekor_check_total.labels(status="failure").inc()
        print(f"Exception running rekor_monitor: {e}")

if __name__ == "__main__":
    while True:
        run_rekor_monitor()
        time.sleep(check_interval)
