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
	netlogLoadgenN        int
	netlogLoadgenDelay    time.Duration
	netlogLoadgenJitter   time.Duration
	netlogLoadgenSyslog   string
	netlogLoadgenProtocol string
)

var netlogLoadgenCmd = &cobra.Command{
	Use:   "loadgen-netlog",
	Short: "Generate random netlog events (network device logs) for testing",
	Long: `Generate random netlog events (Juniper, Cisco, Arista) for testing.

By default, events are inserted directly into PostgreSQL.
Use --syslog to send RFC 5424 messages over UDP/TCP to a rsyslog instance instead,
testing the full ingestion pipeline.`,
	RunE: runNetlogLoadgen,
}

func init() {
	netlogLoadgenCmd.Flags().IntVarP(&netlogLoadgenN, "n", "n", 100, "number of events to insert")
	netlogLoadgenCmd.Flags().DurationVar(&netlogLoadgenDelay, "delay", 0, "fixed delay between inserts (e.g. 100ms)")
	netlogLoadgenCmd.Flags().DurationVar(&netlogLoadgenJitter, "jitter", 0, "random jitter added to delay (e.g. 200ms)")
	netlogLoadgenCmd.Flags().StringVar(&netlogLoadgenSyslog, "syslog", "", "send via syslog instead of SQL (host:port, e.g. localhost:514)")
	netlogLoadgenCmd.Flags().StringVar(&netlogLoadgenProtocol, "protocol", "udp", "syslog transport protocol (udp or tcp)")
}

// vendorWeightTotal is the sum of all vendor weights, computed at init.
var vendorWeightTotal int

func init() {
	for _, v := range vendors {
		vendorWeightTotal += v.weight
	}
}

// pickVendor selects a vendor profile using weighted random selection.
func pickVendor() *vendorProfile {
	n := rand.IntN(vendorWeightTotal)
	for i := range vendors {
		n -= vendors[i].weight
		if n < 0 {
			return &vendors[i]
		}
	}
	return &vendors[len(vendors)-1]
}

// severityWeightTotal is the sum of all severity weights, computed at init.
var severityWeightTotal int

func init() {
	for _, sw := range severityWeights {
		severityWeightTotal += sw.weight
	}
}

// pickSeverity selects a severity level using weighted random selection.
func pickSeverity() int {
	n := rand.IntN(severityWeightTotal)
	for _, sw := range severityWeights {
		n -= sw.weight
		if n < 0 {
			return sw.severity
		}
	}
	return 6 // default to info
}

// generateTag creates a syslog tag from a program name with a random PID.
func generateTag(program string) string {
	pid := 1000 + rand.IntN(64536)
	return fmt.Sprintf("%s[%d]:", program, pid)
}

// syslogPriority computes the PRI value: facility * 8 + severity.
func syslogPriority(facility, severity int) int {
	return facility*8 + severity
}

// formatRFC5424 builds an RFC 5424 syslog message with optional structured data.
func formatRFC5424(facility, severity int, hostname, program, msgid, message string) []byte {
	pri := syslogPriority(facility, severity)
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	if msgid == "" {
		msgid = "-"
	}
	// RFC 5424: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID SD MSG
	pid := 1000 + rand.IntN(64536)
	return fmt.Appendf(nil, "<%d>1 %s %s %s %d %s - %s\n", pri, ts, hostname, program, pid, msgid, message)
}

func runNetlogLoadgen(_ *cobra.Command, _ []string) error {
	if netlogLoadgenSyslog != "" {
		return runNetlogLoadgenSyslog()
	}
	return runNetlogLoadgenSQL()
}

func runNetlogLoadgenSyslog() error {
	if netlogLoadgenProtocol != "udp" && netlogLoadgenProtocol != "tcp" {
		return fmt.Errorf("unsupported protocol %q: use udp or tcp", netlogLoadgenProtocol)
	}

	// Use udp4/tcp4 to avoid IPv6 issues on macOS where Docker
	// maps ports on 0.0.0.0 but Go's dialer prefers [::1].
	network := netlogLoadgenProtocol + "4"

	fmt.Printf("connecting to %s://%s...\n", netlogLoadgenProtocol, netlogLoadgenSyslog)

	ctx := context.Background()
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, netlogLoadgenSyslog)
	if err != nil {
		return fmt.Errorf("dial %s: %w", netlogLoadgenSyslog, err)
	}
	defer func() { _ = conn.Close() }()

	fmt.Printf("connected\n")

	if netlogLoadgenDelay > 0 || netlogLoadgenJitter > 0 {
		fmt.Printf("sending %d syslog messages (delay=%s jitter=%s)...\n", netlogLoadgenN, netlogLoadgenDelay, netlogLoadgenJitter)
	} else {
		fmt.Printf("sending %d syslog messages as fast as possible...\n", netlogLoadgenN)
	}

	start := time.Now()
	for i := range netlogLoadgenN {
		v := pickVendor()
		host := v.hostnames[rand.IntN(len(v.hostnames))]
		prog := v.programs[rand.IntN(len(v.programs))]
		msg := v.messages[rand.IntN(len(v.messages))]
		sev := pickSeverity()
		fac := v.facilities[rand.IntN(len(v.facilities))]

		pkt := formatRFC5424(fac, sev, host, prog, msg.msgid, msg.message)

		if _, err := conn.Write(pkt); err != nil {
			return fmt.Errorf("send %d: %w", i, err)
		}

		if (i+1)%10 == 0 {
			fmt.Printf("  %d/%d (%.0f msgs/sec)\n", i+1, netlogLoadgenN, float64(i+1)/time.Since(start).Seconds())
		}

		if wait := netlogLoadgenDelay + time.Duration(rand.Int64N(int64(netlogLoadgenJitter+1))); wait > 0 {
			time.Sleep(wait)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("done: %d messages in %s (%.0f msgs/sec)\n", netlogLoadgenN, elapsed, float64(netlogLoadgenN)/elapsed.Seconds())
	return nil
}

func runNetlogLoadgenSQL() error {
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
		INSERT INTO netlog_events
			(reported_at, hostname, fromhost_ip, programname, msgid, severity, facility, syslogtag, message)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	if netlogLoadgenDelay > 0 || netlogLoadgenJitter > 0 {
		fmt.Printf("inserting %d events (delay=%s jitter=%s)...\n", netlogLoadgenN, netlogLoadgenDelay, netlogLoadgenJitter)
	} else {
		fmt.Printf("inserting %d events as fast as possible...\n", netlogLoadgenN)
	}

	start := time.Now()
	for i := range netlogLoadgenN {
		v := pickVendor()
		host := v.hostnames[rand.IntN(len(v.hostnames))]
		prog := v.programs[rand.IntN(len(v.programs))]
		msg := v.messages[rand.IntN(len(v.messages))]
		tag := generateTag(prog)
		sev := pickSeverity()
		fac := v.facilities[rand.IntN(len(v.facilities))]
		ip := fmt.Sprintf("%s.%d", v.ipPrefix, rand.IntN(256))

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
			fmt.Printf("  %d/%d (%.0f events/sec)\n", i+1, netlogLoadgenN, float64(i+1)/time.Since(start).Seconds())
		}

		if wait := netlogLoadgenDelay + time.Duration(rand.Int64N(int64(netlogLoadgenJitter+1))); wait > 0 {
			time.Sleep(wait)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("done: %d events in %s (%.0f events/sec)\n", netlogLoadgenN, elapsed, float64(netlogLoadgenN)/elapsed.Seconds())
	return nil
}
