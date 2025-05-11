import subprocess, os, time
from opentelemetry import metrics
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
import logging

# Configure logging
log_level = os.environ.get("LOG_LEVEL", "INFO").upper()
logging.basicConfig(
    level=getattr(logging, log_level, logging.INFO),
    format='%(asctime)s [%(levelname)s] %(message)s',
)
logger = logging.getLogger(__name__)

otlp_endpoint = os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317")
rekor_server_endpoint = os.environ.get("REKOR_SERVER_ENDPOINT", "https://rekor.sigstore.dev")
check_interval = int(os.environ.get("CHECK_INTERVAL_SECONDS", 1))
checkpoint_dir = os.environ.get("CHECKPOINT_DIR", "/data")
checkpoint_path = os.path.join(checkpoint_dir, "checkpoint_log.txt")

# Configure OTel
reader = PeriodicExportingMetricReader(
    OTLPMetricExporter(endpoint=otlp_endpoint),
    export_interval_millis=1000
)
metrics.set_meter_provider(MeterProvider(metric_readers=[reader]))
meter = metrics.get_meter(__name__)

consistency_check = meter.create_counter("rekor_consistency_check", unit="1", description="Rekor checkpoint consistency check result")

def run_check():
    result = subprocess.run(["./rekor_monitor", f"--file={checkpoint_path}", "--once=true", f"--url={rekor_server_endpoint}"], capture_output=True, text=True)
    output = result.stdout + result.stderr
    logger.debug("Command output: %s", output)

    if "consistency verified" in output:
        logger.info("Rekor consistency check: SUCCESS")
        consistency_check.add(1, {"status": "success"})
    else:
        logger.warning("Rekor consistency check: FAILURE")
        consistency_check.add(1, {"status": "failure"})

while True:
    run_check()
    time.sleep(check_interval)
