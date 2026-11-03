package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// statsHostCount is more hostnames than any stats response may break out.
const statsHostCount = model.VolumeGroupLimit + 1000

// seedManyHosts writes one srvlog row for each of statsHostCount hostnames
// (cap-0000 ...), an extra error row for cap-0000..cap-0009, and netlog rows
// for cap-0000 (both feeds) and net-only (netlog only). It returns the number
// of rows written.
func seedManyHosts(t *testing.T, store *Store) int {
	t.Helper()
	ctx := context.Background()
	at := time.Now().UTC().Add(-10 * time.Minute)
	if _, err := store.pool.Exec(ctx,
		`INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
		 SELECT $1, $1, format('cap-%s', lpad(i::text, 4, '0')), '10.0.0.2'::inet, 6, 1, 'seeded'
		 FROM generate_series(0, $2 - 1) AS i`, at, statsHostCount); err != nil {
		t.Fatalf("seed hosts: %v", err)
	}
	if _, err := store.pool.Exec(ctx,
		`INSERT INTO srvlog_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message)
		 SELECT $1, $1, format('cap-%s', lpad(i::text, 4, '0')), '10.0.0.2'::inet, 3, 1, 'seeded error'
		 FROM generate_series(0, 9) AS i`, at); err != nil {
		t.Fatalf("seed errors: %v", err)
	}
	for _, host := range []string{"cap-0000", "net-only"} {
		insertNetlogEvent(t, store.pool, at, host, "", 6, "seeded")
	}
	return statsHostCount + 10 + 2
}

// TestIntegration_VolumeCapsHostGroups seeds more hostnames than the group
// cap and checks the volume response breaks out at most the cap, with the
// rest counted in other and no event lost.
func TestIntegration_VolumeCapsHostGroups(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events", "netlog_events")
	store := NewStore(pool)
	ctx := context.Background()
	rows := seedManyHosts(t, store)
	srvlogRows := int64(rows - 2)

	p := model.VolumeParams{Interval: model.Interval1Hour, RangeDur: 24 * time.Hour}
	buckets, err := store.GetVolume(ctx, p)
	if err != nil {
		t.Fatalf("GetVolume: %v", err)
	}
	groups := map[string]bool{}
	var total, other, broken int64
	for _, b := range buckets {
		var sum int64
		for host, n := range b.ByHost {
			groups[host] = true
			sum += n
		}
		if b.Total != sum+b.Other {
			t.Errorf("bucket %s: total %d != by_host %d + other %d", b.Time, b.Total, sum, b.Other)
		}
		total += b.Total
		other += b.Other
		broken += sum
	}
	if len(groups) == 0 || len(groups) > model.VolumeGroupLimit {
		t.Errorf("by_host holds %d distinct hosts, want 1..%d", len(groups), model.VolumeGroupLimit)
	}
	if total != srvlogRows {
		t.Errorf("total = %d, want every seeded srvlog row (%d)", total, srvlogRows)
	}
	if other == 0 || other != total-broken {
		t.Errorf("other = %d, want total %d - by_host %d > 0", other, total, broken)
	}
	// The ten hosts with an extra error row have the most events.
	for _, b := range buckets {
		if _, ok := b.ByHost["cap-0009"]; !ok && len(b.ByHost) > 0 {
			t.Errorf("bucket %s: top host cap-0009 missing from by_host", b.Time)
		}
	}

	p.TotalsOnly = true
	totals, err := store.GetVolume(ctx, p)
	if err != nil {
		t.Fatalf("GetVolume totals_only: %v", err)
	}
	var totalsSum int64
	for _, b := range totals {
		if len(b.ByHost) != 0 || b.Other != 0 {
			t.Errorf("totals_only bucket %s has by_host %d keys, other %d; want neither", b.Time, len(b.ByHost), b.Other)
		}
		totalsSum += b.Total
	}
	if totalsSum != srvlogRows {
		t.Errorf("totals_only total = %d, want %d", totalsSum, srvlogRows)
	}
}

// TestIntegration_ListHostsPaginatesAndFilters seeds more hostnames than one
// page and checks the hosts index pages, counts, searches, filters and sorts
// in SQL.
func TestIntegration_ListHostsPaginatesAndFilters(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "srvlog_events", "netlog_events")
	store := NewStore(pool)
	ctx := context.Background()
	seedManyHosts(t, store)
	allHosts := statsHostCount + 1 // plus net-only

	base := model.HostQuery{RangeDur: 24 * time.Hour, Sort: "errors", Limit: model.HostsDefaultLimit}
	list := func(t *testing.T, mod func(*model.HostQuery)) model.HostPage {
		t.Helper()
		q := base
		mod(&q)
		page, err := store.ListHosts(ctx, q)
		if err != nil {
			t.Fatalf("ListHosts(%+v): %v", q, err)
		}
		return page
	}

	t.Run("first page is one page of all hosts", func(t *testing.T) {
		page := list(t, func(*model.HostQuery) {})
		if len(page.Hosts) != model.HostsDefaultLimit || page.Total != allHosts {
			t.Fatalf("got %d hosts, total %d; want %d, total %d", len(page.Hosts), page.Total, model.HostsDefaultLimit, allHosts)
		}
		// Sorted by errors: the ten hosts with an error row come first.
		for i, h := range page.Hosts[:10] {
			if h.ErrorCount != 1 || h.Status != model.HostStatusCritical {
				t.Errorf("host %d %s: errors %d status %s, want 1 critical", i, h.Hostname, h.ErrorCount, h.Status)
			}
		}
		h := page.Hosts[0]
		if h.Hostname != "cap-0000" || h.Feed != "both" || h.TotalCount != 3 || h.LastSeenAt == nil ||
			len(h.SeverityBreakdown) != 2 || len(h.HourlyBuckets) == 0 {
			t.Errorf("first host = %+v, want cap-0000 on both feeds with 3 events and details", h)
		}
	})

	t.Run("pages do not overlap and the last page is short", func(t *testing.T) {
		seen := map[string]bool{}
		for offset := 0; offset < allHosts; offset += model.HostsMaxLimit {
			page := list(t, func(q *model.HostQuery) { q.Sort = "hostname"; q.Limit = model.HostsMaxLimit; q.Offset = offset })
			for _, h := range page.Hosts {
				if seen[h.Hostname] {
					t.Fatalf("host %s returned on two pages", h.Hostname)
				}
				seen[h.Hostname] = true
			}
		}
		if len(seen) != allHosts {
			t.Errorf("paged through %d hosts, want %d", len(seen), allHosts)
		}
	})

	t.Run("offset past the end keeps the total", func(t *testing.T) {
		page := list(t, func(q *model.HostQuery) { q.Offset = allHosts })
		if len(page.Hosts) != 0 || page.Total != allHosts {
			t.Errorf("got %d hosts, total %d; want 0, total %d", len(page.Hosts), page.Total, allHosts)
		}
	})

	filters := []struct {
		name  string
		mod   func(*model.HostQuery)
		total int
		first string
	}{
		{"search is a case-insensitive substring", func(q *model.HostQuery) { q.Search = "CAP-001"; q.Sort = "hostname" }, 10, "cap-0010"},
		{"search treats LIKE metacharacters literally", func(q *model.HostQuery) { q.Search = "cap_" }, 0, ""},
		{"status filter", func(q *model.HostQuery) { q.Status = model.HostStatusCritical; q.Sort = "hostname" }, 10, "cap-0000"},
		{"feed both", func(q *model.HostQuery) { q.Feed = "both" }, 1, "cap-0000"},
		{"feed netlog", func(q *model.HostQuery) { q.Feed = "netlog" }, 1, "net-only"},
		{"sort by total", func(q *model.HostQuery) { q.Sort = "total" }, allHosts, "cap-0000"},
		// Every host was last seen at the same instant and has no previous
		// period, so these tie and fall back to hostname.
		{"sort by last seen", func(q *model.HostQuery) { q.Sort = "last_seen" }, allHosts, "cap-0000"},
		{"sort by trend", func(q *model.HostQuery) { q.Sort = "trend" }, allHosts, "cap-0000"},
	}
	for _, tt := range filters {
		t.Run(tt.name, func(t *testing.T) {
			page := list(t, tt.mod)
			if page.Total != tt.total {
				t.Fatalf("total = %d, want %d", page.Total, tt.total)
			}
			if tt.first != "" && (len(page.Hosts) == 0 || page.Hosts[0].Hostname != tt.first) {
				t.Errorf("first host = %v, want %s", page.Hosts, tt.first)
			}
		})
	}
}
