package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/internal/config"
)

var (
	loadgenN      int
	loadgenDelay  time.Duration
	loadgenJitter time.Duration
)

var loadgenCmd = &cobra.Command{
	Use:   "loadgen",
	Short: "Generate random syslog events for testing",
	RunE:  runLoadgen,
}

func init() {
	loadgenCmd.Flags().IntVarP(&loadgenN, "n", "n", 100, "number of events to insert")
	loadgenCmd.Flags().DurationVar(&loadgenDelay, "delay", 0, "fixed delay between inserts (e.g. 100ms)")
	loadgenCmd.Flags().DurationVar(&loadgenJitter, "jitter", 0, "random jitter added to delay (e.g. 200ms)")
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

func runLoadgen(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
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
		INSERT INTO syslog_events
			(reported_at, hostname, fromhost_ip, programname, msgid, severity, facility, syslogtag, message)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	if loadgenDelay > 0 || loadgenJitter > 0 {
		fmt.Printf("inserting %d events (delay=%s jitter=%s)...\n", loadgenN, loadgenDelay, loadgenJitter)
	} else {
		fmt.Printf("inserting %d events as fast as possible...\n", loadgenN)
	}

	start := time.Now()
	for i := range loadgenN {
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
			fmt.Printf("  %d/%d (%.0f events/sec)\n", i+1, loadgenN, float64(i+1)/time.Since(start).Seconds())
		}

		if wait := loadgenDelay + time.Duration(rand.Int64N(int64(loadgenJitter+1))); wait > 0 {
			time.Sleep(wait)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("done: %d events in %s (%.0f events/sec)\n", loadgenN, elapsed, float64(loadgenN)/elapsed.Seconds())
	return nil
}
