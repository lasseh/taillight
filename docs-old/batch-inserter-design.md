# Future Improvements

## Replace ompgsql with omprog batch inserter

**Problem:** rsyslog's `ompgsql` module executes one INSERT statement per syslog
message. At 5000 msg/sec this means 5000 individual round-trips to PostgreSQL,
each with its own transaction overhead.

**Solution:** Use `omprog` to call a custom batch inserter (Go or Python) that
buffers messages and uses multi-row INSERT or the COPY protocol.

### Expected improvement

- Individual INSERTs: ~5000 transactions/sec, high WAL overhead
- Multi-row INSERT (128 rows/batch): ~40 transactions/sec, 10-50x less overhead
- COPY protocol: single stream, highest throughput

### Implementation sketch

```
rsyslog omprog → stdin → batch-inserter → PostgreSQL COPY/multi-INSERT
```

The batch inserter would:
1. Read JSON-formatted syslog events from stdin (one per line)
2. Buffer up to N messages or T milliseconds (whichever comes first)
3. Execute a single multi-row INSERT or COPY INTO syslog_events
4. Report success/failure back to rsyslog via stdout

### rsyslog config change

```rsyslog
# Replace ompgsql output with omprog batch inserter
ruleset(name="output_pgsql") {
    action(
        type="omprog"
        binary="/usr/local/bin/syslog-batch-insert --db postgres://..."
        template="JSONFormat"
        output="/var/log/syslog-batch-insert.log"
        queue.type="LinkedList"
        queue.size="50000"
        queue.filename="pgsql_batch_queue"
        queue.maxDiskSpace="2G"
        queue.saveOnShutdown="on"
        queue.dequeueBatchSize="128"
        action.resumeRetryCount="-1"
        action.resumeInterval="30"
    )
}
```

### Considerations

- Need to handle rsyslog's omprog protocol (confirm/deny processing)
- Buffer flush timeout should be ~100-500ms to balance latency vs throughput
- Need graceful shutdown (flush buffer on SIGTERM)
- The batch inserter could be built as a separate Go binary in this repo
- COPY protocol requires careful error handling (entire batch fails on one bad row)
