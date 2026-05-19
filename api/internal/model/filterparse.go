package model

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// queryParams parses and validates HTTP query parameters for log filters,
// accumulating one error message per invalid parameter. It centralises the
// validation rules (length bounds, integer ranges, IP and RFC3339 parsing)
// that were previously copy-pasted across ParseSrvlogFilter / ParseNetlogFilter
// / ParseAppLogFilter, so a fix or a new rule lands in exactly one place and is
// directly unit-testable.
type queryParams struct {
	q    url.Values
	errs []string
}

func newQueryParams(r *http.Request) *queryParams {
	return &queryParams{q: r.URL.Query()}
}

// str returns the raw value for key, recording an error if it exceeds
// maxFilterStringLen.
func (p *queryParams) str(key string) string {
	v := p.q.Get(key)
	if len(v) > maxFilterStringLen {
		p.errs = append(p.errs, fmt.Sprintf("%s: exceeds max length %d", key, maxFilterStringLen))
		return ""
	}
	return v
}

// boundedInt returns a pointer to the integer value for key, or nil if absent.
// Records an error if the value is not an integer within [min, max].
func (p *queryParams) boundedInt(key string, minVal, maxVal int) *int {
	v := p.q.Get(key)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < minVal || n > maxVal {
		p.errs = append(p.errs, fmt.Sprintf("%s: must be an integer %d-%d", key, minVal, maxVal))
		return nil
	}
	return &n
}

// ip returns the normalised IP for key, or "" if absent. Records an error if
// the value is not a valid IP address.
func (p *queryParams) ip(key string) string {
	v := p.q.Get(key)
	if v == "" {
		return ""
	}
	addr, err := netip.ParseAddr(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Sprintf("%s: must be a valid IP address", key))
		return ""
	}
	return addr.String()
}

// rfc3339 returns a pointer to the time for key, or nil if absent. Records an
// error if the value is not RFC3339.
func (p *queryParams) rfc3339(key string) *time.Time {
	v := p.q.Get(key)
	if v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		p.errs = append(p.errs, fmt.Sprintf("%s: must be RFC3339 format", key))
		return nil
	}
	return &t
}

// fail records a caller-supplied error message for key.
func (p *queryParams) fail(msg string) {
	p.errs = append(p.errs, msg)
}

// err returns the accumulated validation error, or nil if all parameters were
// valid.
func (p *queryParams) err() error {
	if len(p.errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid query parameters: %s", strings.Join(p.errs, "; "))
}
