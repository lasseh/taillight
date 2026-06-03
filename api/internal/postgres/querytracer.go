package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/metrics"
)

// QueryTracer is a pgx.QueryTracer that records per-operation query latency and
// error counts. Attach it to a pool via poolCfg.ConnConfig.Tracer to instrument
// every store call from one seam, without touching each query site (audit N1).
type QueryTracer struct{}

type queryTraceKey struct{}

type queryTraceInfo struct {
	op    string
	start time.Time
}

// TraceQueryStart records the operation and start time on the context.
func (QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryTraceKey{}, queryTraceInfo{op: queryOp(data.SQL), start: time.Now()})
}

// TraceQueryEnd observes the latency and counts errors (excluding no-rows).
func (QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	info, ok := ctx.Value(queryTraceKey{}).(queryTraceInfo)
	if !ok {
		return
	}
	metrics.DBQueryDuration.WithLabelValues(info.op).Observe(time.Since(info.start).Seconds())
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		metrics.DBQueryErrorsTotal.WithLabelValues(info.op).Inc()
	}
}

// queryOp extracts a bounded operation label from a SQL statement: the leading
// keyword if it is one of a known set, otherwise "other". This keeps metric
// cardinality fixed regardless of the statement text.
func queryOp(sql string) string {
	s := strings.TrimSpace(sql)
	end := strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == '('
	})
	if end > 0 {
		s = s[:end]
	}
	switch strings.ToUpper(s) {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH",
		"CREATE", "DROP", "ALTER", "REFRESH", "CALL", "TRUNCATE":
		return strings.ToUpper(s)
	default:
		return "other"
	}
}
