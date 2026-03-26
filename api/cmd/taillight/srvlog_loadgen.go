package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/internal/config"
)

var (
	srvlogLoadgenN        int
	srvlogLoadgenDelay    time.Duration
	srvlogLoadgenJitter   time.Duration
	srvlogLoadgenSyslog   string
	srvlogLoadgenProtocol string
)

var srvlogLoadgenCmd = &cobra.Command{
	Use:   "loadgen-srvlog",
	Short: "Generate random srvlog events (server logs) for testing",
	Long: `Generate random srvlog events (Linux, nginx, PostgreSQL, Docker) for testing.

By default, events are inserted directly into PostgreSQL.
Use --syslog to send RFC 5424 messages over UDP/TCP to a rsyslog instance instead,
testing the full ingestion pipeline.`,
	RunE: runSrvlogLoadgen,
}

func init() {
	srvlogLoadgenCmd.Flags().IntVarP(&srvlogLoadgenN, "n", "n", 100, "number of events to insert")
	srvlogLoadgenCmd.Flags().DurationVar(&srvlogLoadgenDelay, "delay", 0, "fixed delay between inserts (e.g. 100ms)")
	srvlogLoadgenCmd.Flags().DurationVar(&srvlogLoadgenJitter, "jitter", 0, "random jitter added to delay (e.g. 200ms)")
	srvlogLoadgenCmd.Flags().StringVar(&srvlogLoadgenSyslog, "syslog", "", "send via syslog instead of SQL (host:port, e.g. localhost:514)")
	srvlogLoadgenCmd.Flags().StringVar(&srvlogLoadgenProtocol, "protocol", "udp", "syslog transport protocol (udp or tcp)")
}

// srvlogServerWeightTotal is the sum of all server profile weights, computed at init.
var srvlogServerWeightTotal int

func init() {
	for _, s := range serverProfiles {
		srvlogServerWeightTotal += s.weight
	}
}

// pickServer selects a server profile using weighted random selection.
func pickServer() *serverProfile {
	n := rand.IntN(srvlogServerWeightTotal)
	for i := range serverProfiles {
		n -= serverProfiles[i].weight
		if n < 0 {
			return &serverProfiles[i]
		}
	}
	return &serverProfiles[len(serverProfiles)-1]
}

// srvlogSeverityWeightTotal is the sum of all srvlog severity weights, computed at init.
var srvlogSeverityWeightTotal int

func init() {
	for _, sw := range srvlogSeverityWeights {
		srvlogSeverityWeightTotal += sw.weight
	}
}

// pickSrvlogSeverity selects a severity level using weighted random selection.
func pickSrvlogSeverity() int {
	n := rand.IntN(srvlogSeverityWeightTotal)
	for _, sw := range srvlogSeverityWeights {
		n -= sw.weight
		if n < 0 {
			return sw.severity
		}
	}
	return 6 // default to info
}

func runSrvlogLoadgen(_ *cobra.Command, _ []string) error {
	if srvlogLoadgenSyslog != "" {
		return runSrvlogLoadgenSyslog()
	}
	return runSrvlogLoadgenSQL()
}

func runSrvlogLoadgenSyslog() error {
	if srvlogLoadgenProtocol != "udp" && srvlogLoadgenProtocol != "tcp" {
		return fmt.Errorf("unsupported protocol %q: use udp or tcp", srvlogLoadgenProtocol)
	}

	network := srvlogLoadgenProtocol + "4"

	fmt.Printf("connecting to %s://%s...\n", srvlogLoadgenProtocol, srvlogLoadgenSyslog)

	ctx := context.Background()
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, srvlogLoadgenSyslog)
	if err != nil {
		return fmt.Errorf("dial %s: %w", srvlogLoadgenSyslog, err)
	}
	defer func() { _ = conn.Close() }()

	fmt.Printf("connected\n")

	if srvlogLoadgenDelay > 0 || srvlogLoadgenJitter > 0 {
		fmt.Printf("sending %d syslog messages (delay=%s jitter=%s)...\n", srvlogLoadgenN, srvlogLoadgenDelay, srvlogLoadgenJitter)
	} else {
		fmt.Printf("sending %d syslog messages as fast as possible...\n", srvlogLoadgenN)
	}

	start := time.Now()
	for i := range srvlogLoadgenN {
		srv := pickServer()
		host := srv.hostnames[rand.IntN(len(srv.hostnames))]
		prog := srv.programs[rand.IntN(len(srv.programs))]
		msg := srv.messages[rand.IntN(len(srv.messages))]
		sev := pickSrvlogSeverity()
		fac := srv.facilities[rand.IntN(len(srv.facilities))]

		pkt := formatRFC5424(fac, sev, host, prog, msg.msgid, msg.message)

		if _, err := conn.Write(pkt); err != nil {
			return fmt.Errorf("send %d: %w", i, err)
		}

		if (i+1)%10 == 0 {
			fmt.Printf("  %d/%d (%.0f msgs/sec)\n", i+1, srvlogLoadgenN, float64(i+1)/time.Since(start).Seconds())
		}

		if wait := srvlogLoadgenDelay + time.Duration(rand.Int64N(int64(srvlogLoadgenJitter+1))); wait > 0 {
			time.Sleep(wait)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("done: %d messages in %s (%.0f msgs/sec)\n", srvlogLoadgenN, elapsed, float64(srvlogLoadgenN)/elapsed.Seconds())
	return nil
}

func runSrvlogLoadgenSQL() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Printf("connecting to database...\n")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	fmt.Printf("connected\n")

	const query = `
		INSERT INTO srvlog_events
			(reported_at, hostname, fromhost_ip, programname, msgid, severity, facility, syslogtag, message)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	if srvlogLoadgenDelay > 0 || srvlogLoadgenJitter > 0 {
		fmt.Printf("inserting %d events (delay=%s jitter=%s)...\n", srvlogLoadgenN, srvlogLoadgenDelay, srvlogLoadgenJitter)
	} else {
		fmt.Printf("inserting %d events as fast as possible...\n", srvlogLoadgenN)
	}

	start := time.Now()
	for i := range srvlogLoadgenN {
		srv := pickServer()
		host := srv.hostnames[rand.IntN(len(srv.hostnames))]
		prog := srv.programs[rand.IntN(len(srv.programs))]
		msg := srv.messages[rand.IntN(len(srv.messages))]
		tag := generateTag(prog)
		sev := pickSrvlogSeverity()
		fac := srv.facilities[rand.IntN(len(srv.facilities))]
		ip := fmt.Sprintf("%s.%d", srv.ipPrefix, rand.IntN(256))

		_, err := conn.Exec(ctx, query,
			time.Now(),
			host,
			ip,
			prog,
			msg.msgid,
			sev,
			fac,
			tag,
			msg.message,
		)
		if err != nil {
			return fmt.Errorf("insert %d: %w", i, err)
		}

		if (i+1)%10 == 0 {
			fmt.Printf("  %d/%d (%.0f events/sec)\n", i+1, srvlogLoadgenN, float64(i+1)/time.Since(start).Seconds())
		}

		if wait := srvlogLoadgenDelay + time.Duration(rand.Int64N(int64(srvlogLoadgenJitter+1))); wait > 0 {
			time.Sleep(wait)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("done: %d events in %s (%.0f events/sec)\n", srvlogLoadgenN, elapsed, float64(srvlogLoadgenN)/elapsed.Seconds())
	return nil
}
