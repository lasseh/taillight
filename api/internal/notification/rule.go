package notification

import (
	"strings"

	"github.com/lasseh/taillight/internal/model"
)

// SrvlogFilter converts the rule's filter fields to a model.SrvlogFilter
// for reuse of the existing Matches() logic.
func (r Rule) SrvlogFilter() model.SrvlogFilter {
	return model.SrvlogFilter{
		Hostname:    r.Hostname,
		Programname: r.Programname,
		Severity:    r.Severity,
		SeverityMax: r.SeverityMax,
		Facility:    r.Facility,
		SyslogTag:   r.SyslogTag,
		MsgID:       r.MsgID,
		Search:      r.Search,
	}
}

// AppLogFilter converts the rule's filter fields to a model.AppLogFilter.
func (r Rule) AppLogFilter() model.AppLogFilter {
	return model.AppLogFilter{
		Service:   r.Service,
		Component: r.Component,
		Host:      r.Host,
		Level:     r.Level,
		Search:    r.Search,
	}
}

// MatchesSrvlog reports whether the event satisfies this rule's srvlog filter.
func (r Rule) MatchesSrvlog(e model.SrvlogEvent) bool {
	return r.SrvlogFilter().Matches(e)
}

// MatchesAppLog reports whether the event satisfies this rule's applog filter.
func (r Rule) MatchesAppLog(e model.AppLogEvent) bool {
	return r.AppLogFilter().Matches(e)
}

// GroupKeyFromSrvlog extracts a group key from a srvlog event based on the
// rule's GroupBy field. Default grouping is by hostname.
func (r Rule) GroupKeyFromSrvlog(e model.SrvlogEvent) string {
	fields := r.groupByFields("hostname")
	var parts []string
	for _, f := range fields {
		switch f {
		case "hostname":
			parts = append(parts, e.Hostname)
		case "programname":
			parts = append(parts, e.Programname)
		case "syslogtag":
			parts = append(parts, e.SyslogTag)
		case "severity":
			parts = append(parts, e.SeverityLabel)
		default:
			parts = append(parts, e.Hostname)
		}
	}
	return strings.Join(parts, "|")
}

// GroupKeyFromAppLog extracts a group key from an applog event based on the
// rule's GroupBy field. Default grouping is by host.
func (r Rule) GroupKeyFromAppLog(e model.AppLogEvent) string {
	fields := r.groupByFields("host")
	var parts []string
	for _, f := range fields {
		switch f {
		case "host":
			parts = append(parts, e.Host)
		case "service":
			parts = append(parts, e.Service)
		case "component":
			parts = append(parts, e.Component)
		case "level":
			parts = append(parts, e.Level)
		default:
			parts = append(parts, e.Host)
		}
	}
	return strings.Join(parts, "|")
}

// groupByFields parses the GroupBy field into individual field names.
func (r Rule) groupByFields(defaultField string) []string {
	gb := strings.TrimSpace(r.GroupBy)
	if gb == "" {
		return []string{defaultField}
	}
	var fields []string
	for f := range strings.SplitSeq(gb, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return []string{defaultField}
	}
	return fields
}
