# taillight-shipper

A log shipping tool that reads log lines from stdin and/or tails log files, forwarding them to a [taillight](../../) ingest endpoint.

## Modes

| Mode | Command | Description |
|------|---------|-------------|
| Stdin pipe | `./app \| taillight-shipper -c config.yaml` | Read lines from a piped process |
| File follow | `taillight-shipper -c config.yaml` | Tail one or more log files |
| Both | `./app \| taillight-shipper -c config.yaml -t` | Pipe stdin + tail files simultaneously |

Stdin is auto-detected. If stdin is connected to a pipe, it is read. If `files` are configured, they are tailed. Both can run at the same time.

## Build

```sh
make build-shipper
```

## Usage

```
taillight-shipper --config <config.yaml> [--tee]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--config` | `-c` | Path to config file (required) |
| `--tee` | `-t` | Tee mode: echo each stdin line to stdout (useful for chaining) |

## Configuration

See [config.example.yaml](config.example.yaml) for a full example.

```yaml
endpoint: http://localhost:8080/api/v1/applog/ingest
api_key: ""
service: my-app
component: ""
host: ""
batch_size: 100
flush_period: 1s
buffer_size: 1024

files:
  - path: /var/log/myapp/api.log
    service: myapp-api
    component: http
    host: api-prod-1
  - path: /var/log/myapp/worker.log
    service: myapp-worker
    component: jobs
```

| Field | Default | Description |
|-------|---------|-------------|
| `endpoint` | — | Taillight ingest URL |
| `api_key` | `""` | Bearer token for authentication |
| `service` | — | Default service name (used for stdin, fallback for files) |
| `component` | `""` | Default component name |
| `host` | `os.Hostname()` | Host identifier (falls back to system hostname) |
| `batch_size` | `100` | Flush when batch reaches this size |
| `flush_period` | `1s` | Flush at least this often (Go duration) |
| `buffer_size` | `1024` | Buffered channel capacity per handler |
| `files` | `[]` | List of files to tail |

Each file entry can override `service`, `component`, and `host`. If omitted, the top-level values are used as defaults.

## Line Parsing

Each line is parsed as JSON first. If that fails, it's treated as plain text with `INFO` level and the current timestamp.

For JSON lines, the following fields are extracted:

| JSON field | Maps to |
|------------|---------|
| `time` or `timestamp` | Record timestamp (RFC 3339) |
| `level` | Log level (`DEBUG`, `INFO`, `WARN`/`WARNING`, `ERROR`) |
| `msg` or `message` | Log message |
| All other fields | Stored as structured attributes |

## File Tailing

Files are tailed using [nxadm/tail](https://github.com/nxadm/tail) with the following behavior:

- **Follow** — continuously reads new lines as they are appended
- **Rotation** — re-opens the file at the same path after logrotate or similar tools rename it
- **Missing files** — waits for the file to appear if it doesn't exist at startup
- **Seek to end** — starts reading from the end of the file, only shipping new lines

## Shutdown

On `SIGINT` or `SIGTERM`:

1. All goroutines (stdin reader + file tailers) are cancelled
2. Each handler is flushed with a 5-second timeout
3. Any dropped log entries are reported to stderr

## Examples

Ship a process's stdout:

```sh
./my-api | taillight-shipper -c config.yaml
```

Ship stdout while keeping terminal output:

```sh
./my-api | taillight-shipper -c config.yaml -t
```

Tail multiple log files (no stdin):

```sh
taillight-shipper -c config.yaml
```

Pipe stdin and tail files simultaneously:

```sh
./my-api | taillight-shipper -c config.yaml -t
# (with files configured in config.yaml)
```
