package notification

import "github.com/lasseh/taillight/internal/model"

// SyslogFilter converts the rule's filter fields to a model.SyslogFilter
// for reuse of the existing Matches() logic.
func (r Rule) SyslogFilter() model.SyslogFilter {
	return model.SyslogFilter{
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

// MatchesSyslog reports whether the event satisfies this rule's syslog filter.
func (r Rule) MatchesSyslog(e model.SyslogEvent) bool {
	return r.SyslogFilter().Matches(e)
}

// MatchesAppLog reports whether the event satisfies this rule's applog filter.
func (r Rule) MatchesAppLog(e model.AppLogEvent) bool {
	return r.AppLogFilter().Matches(e)
}
