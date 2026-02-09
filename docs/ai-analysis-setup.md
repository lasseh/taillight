# AI Analysis Setup (macOS)

Taillight includes an optional AI analysis feature that generates daily operational briefings from your syslog data using a local [Ollama](https://ollama.com) instance. No data leaves your machine.

## Prerequisites

- macOS with [Homebrew](https://brew.sh) installed
- Taillight running with syslog data flowing in

## Install Ollama

```bash
brew install ollama
```

Start the Ollama service:

```bash
brew services start ollama
```

Verify it's running:

```bash
curl -s http://localhost:11434/api/tags | jq .
```

## Pull a Model

Pick a model based on your hardware. Ollama runs inference on your Mac's GPU (Apple Silicon) or CPU.

| Model | RAM Needed | Quality | Speed |
|-------|-----------|---------|-------|
| `llama3.1:8b` | ~6 GB | Good for daily briefings | Fast |
| `llama3.1:70b` | ~48 GB | Best quality | Slow |
| `mixtral:8x7b` | ~32 GB | Strong reasoning | Medium |
| `gemma3:12b` | ~10 GB | Good balance | Fast |

Pull your chosen model:

```bash
ollama pull llama3.1:8b
```

## Configure Taillight

Edit your `config.yaml` (in the `api/` directory):

```yaml
analysis:
  enabled: true
  ollama_url: "http://localhost:11434"
  model: "llama3.1:8b"
  schedule_at: "06:00"    # Daily run time in UTC
  temperature: 0.3        # Low = factual, high = creative
  num_ctx: 8192           # Context window (tokens)
```

Or use environment variables:

```bash
export ANALYSIS_ENABLED=true
export ANALYSIS_OLLAMA_URL=http://localhost:11434
export ANALYSIS_MODEL=llama3.1:8b
export ANALYSIS_SCHEDULE_AT=06:00
export ANALYSIS_TEMPERATURE=0.3
export ANALYSIS_NUM_CTX=8192
```

### Configuration Reference

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `false` | Enable the analysis feature |
| `ollama_url` | `http://localhost:11434` | Ollama API endpoint |
| `model` | `llama3` | Model name (must be pulled first) |
| `schedule_at` | `06:00` | Daily run time in UTC (HH:MM) |
| `temperature` | `0.3` | Sampling temperature (0.0-1.0) |
| `num_ctx` | `8192` | Context window size in tokens |

## How It Works

When enabled, Taillight runs a daily analysis cycle:

1. **Gather** - Aggregates the last 24 hours of syslog data:
   - Top 25 message types by volume with severity breakdown
   - Severity level comparison against 7-day baseline
   - Top 15 hosts with the most errors (severity 0-3)
   - New message types not seen in the prior 7 days
   - Cross-host event clusters (5-minute correlation windows)
   - Juniper syslog reference enrichment (if available)

2. **Analyze** - Sends the aggregated data to Ollama with a system prompt that produces a structured markdown report with:
   - Executive summary
   - Per-event incident analysis
   - Anomaly detection (severity spikes, new event types)
   - Cross-host event correlation
   - Prioritized action items

3. **Store** - Persists the report to the `analysis_reports` table with token counts and duration metrics.

## API Endpoints

### List Reports

```bash
curl http://localhost:8080/api/v1/analysis/reports
```

Optional query parameters: `limit` (default 30, max 100).

### Get Latest Report

```bash
curl http://localhost:8080/api/v1/analysis/reports/latest
```

### Get Report by ID

```bash
curl http://localhost:8080/api/v1/analysis/reports/42
```

### Trigger Analysis Manually

Don't want to wait for the scheduled run:

```bash
curl -X POST http://localhost:8080/api/v1/analysis/reports/trigger
```

Returns `{"report_id": 123}`. The analysis runs synchronously (may take 1-10 minutes depending on model and data volume).

## Monitoring

Prometheus metrics are exposed at `/metrics`:

| Metric | Description |
|--------|-------------|
| `taillight_analysis_runs_total{status="completed"}` | Successful analysis runs |
| `taillight_analysis_runs_total{status="failed"}` | Failed analysis runs |
| `taillight_analysis_duration_seconds` | Run duration histogram |

## Troubleshooting

**"Ollama ping failed"** in logs
- Check Ollama is running: `brew services info ollama`
- Verify the URL: `curl http://localhost:11434/api/tags`

**Analysis takes too long**
- Use a smaller model (`llama3.1:8b` instead of `70b`)
- Reduce `num_ctx` if reports are being truncated
- The 10-minute HTTP timeout is hardcoded; very large models on CPU may exceed it

**Empty or low-quality reports**
- Ensure you have at least 24 hours of syslog data
- Try a larger model for better reasoning
- Lower `temperature` (closer to 0) for more factual output

**Model not found**
- Pull it first: `ollama pull <model-name>`
- List available models: `ollama list`

## Docker Compose

When running via Docker Compose, add an Ollama service or point to your host:

```yaml
# Option 1: Use host Ollama (add to api environment)
environment:
  ANALYSIS_ENABLED: "true"
  ANALYSIS_OLLAMA_URL: "http://host.docker.internal:11434"
  ANALYSIS_MODEL: "llama3.1:8b"

# Option 2: Add Ollama as a service
services:
  ollama:
    image: ollama/ollama
    ports:
      - "11434:11434"
    volumes:
      - ollama_data:/root/.ollama
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia  # Linux GPU only; on macOS use host Ollama
              count: all
              capabilities: [gpu]
```

On macOS, option 1 (`host.docker.internal`) is recommended since Docker containers can't access Apple Silicon GPU acceleration. Run Ollama natively on the host for best performance.
