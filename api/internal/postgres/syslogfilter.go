package postgres

import (
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/lasseh/taillight/internal/model"
)

// syslogFilterClause is the set of filter criteria shared by srvlog and netlog
// (whose model filters are field-identical but distinct types). It exists so
// the WHERE-clause construction — the bug-prone part: escapeLike, wildcard
// ILIKE, the ::inet cast, severity ranges — lives in exactly one place instead
// of being copy-pasted between applySrvlogFilter and applyNetlogFilter. The two
// adapters below are mechanical field maps with no logic; the depth is here.
type syslogFilterClause struct {
	Hostname    string
	FromhostIP  string
	Programname string
	Severity    *int
	SeverityMax *int
	Facility    *int
	SyslogTag   string
	MsgID       string
	Search      string
	From        *time.Time
	To          *time.Time
}

func applySyslogFilter(qb sq.SelectBuilder, f syslogFilterClause) sq.SelectBuilder {
	if f.Hostname != "" {
		if strings.Contains(f.Hostname, "*") {
			pattern := strings.ReplaceAll(escapeLike(f.Hostname), "*", "%")
			qb = qb.Where("hostname ILIKE ?", pattern)
		} else {
			qb = qb.Where(sq.Eq{"hostname": f.Hostname})
		}
	}
	if f.FromhostIP != "" {
		qb = qb.Where("fromhost_ip = ?::inet", f.FromhostIP)
	}
	if f.Programname != "" {
		qb = qb.Where(sq.Eq{"programname": f.Programname})
	}
	if f.Severity != nil {
		qb = qb.Where(sq.Eq{"severity": *f.Severity})
	}
	if f.SeverityMax != nil {
		qb = qb.Where(sq.LtOrEq{"severity": *f.SeverityMax})
	}
	if f.Facility != nil {
		qb = qb.Where(sq.Eq{"facility": *f.Facility})
	}
	if f.SyslogTag != "" {
		qb = qb.Where(sq.Eq{"syslogtag": f.SyslogTag})
	}
	if f.MsgID != "" {
		qb = qb.Where(sq.Eq{"msgid": f.MsgID})
	}
	if f.Search != "" {
		escaped := escapeLike(f.Search)
		qb = qb.Where("message ILIKE ?", "%"+escaped+"%")
	}
	if f.From != nil {
		qb = qb.Where(sq.GtOrEq{"received_at": *f.From})
	}
	if f.To != nil {
		qb = qb.Where(sq.LtOrEq{"received_at": *f.To})
	}
	return qb
}

func applySrvlogFilter(qb sq.SelectBuilder, f model.SrvlogFilter) sq.SelectBuilder {
	return applySyslogFilter(qb, syslogFilterClause{
		Hostname:    f.Hostname,
		FromhostIP:  f.FromhostIP,
		Programname: f.Programname,
		Severity:    f.Severity,
		SeverityMax: f.SeverityMax,
		Facility:    f.Facility,
		SyslogTag:   f.SyslogTag,
		MsgID:       f.MsgID,
		Search:      f.Search,
		From:        f.From,
		To:          f.To,
	})
}

func applyNetlogFilter(qb sq.SelectBuilder, f model.NetlogFilter) sq.SelectBuilder {
	return applySyslogFilter(qb, syslogFilterClause{
		Hostname:    f.Hostname,
		FromhostIP:  f.FromhostIP,
		Programname: f.Programname,
		Severity:    f.Severity,
		SeverityMax: f.SeverityMax,
		Facility:    f.Facility,
		SyslogTag:   f.SyslogTag,
		MsgID:       f.MsgID,
		Search:      f.Search,
		From:        f.From,
		To:          f.To,
	})
}
