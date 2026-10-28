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

// syslogConds returns one condition per set field. The positive filter and
// the mute exclusions both build from it.
func syslogConds(f syslogFilterClause) []sq.Sqlizer {
	var conds []sq.Sqlizer
	if f.Hostname != "" {
		if strings.Contains(f.Hostname, "*") {
			pattern := strings.ReplaceAll(escapeLike(f.Hostname), "*", "%")
			conds = append(conds, sq.Expr("hostname ILIKE ?", pattern))
		} else {
			conds = append(conds, sq.Eq{"hostname": f.Hostname})
		}
	}
	if f.FromhostIP != "" {
		conds = append(conds, sq.Expr("fromhost_ip = ?::inet", f.FromhostIP))
	}
	if f.Programname != "" {
		conds = append(conds, sq.Eq{"programname": f.Programname})
	}
	if f.Severity != nil {
		conds = append(conds, sq.Eq{"severity": *f.Severity})
	}
	if f.SeverityMax != nil {
		conds = append(conds, sq.LtOrEq{"severity": *f.SeverityMax})
	}
	if f.Facility != nil {
		conds = append(conds, sq.Eq{"facility": *f.Facility})
	}
	if f.SyslogTag != "" {
		conds = append(conds, sq.Eq{"syslogtag": f.SyslogTag})
	}
	if f.MsgID != "" {
		conds = append(conds, sq.Eq{"msgid": f.MsgID})
	}
	if f.Search != "" {
		escaped := escapeLike(f.Search)
		conds = append(conds, sq.Expr("message ILIKE ?", "%"+escaped+"%"))
	}
	if f.From != nil {
		conds = append(conds, sq.GtOrEq{"received_at": *f.From})
	}
	if f.To != nil {
		conds = append(conds, sq.LtOrEq{"received_at": *f.To})
	}
	return conds
}

// applySyslogFilter adds each condition as its own WHERE term. It does not
// wrap them in sq.And, because an empty And renders (1=1).
func applySyslogFilter(qb sq.SelectBuilder, f syslogFilterClause) sq.SelectBuilder {
	for _, c := range syslogConds(f) {
		qb = qb.Where(c)
	}
	return qb
}

// excludeSyslog adds NOT (<conds>) for one mute rule, or nothing when f sets
// no field. NOT is only correct because the columns a rule can match
// (hostname, programname, message) are NOT NULL: on a NULL column the inner
// condition would be NULL, NOT (NULL) is NULL, and the row would be dropped
// instead of kept.
func excludeSyslog(qb sq.SelectBuilder, f syslogFilterClause) sq.SelectBuilder {
	conds := syslogConds(f)
	if len(conds) == 0 {
		return qb
	}
	return qb.Where(sq.Expr("NOT ?", sq.And(conds)))
}

func srvlogClause(f model.SrvlogFilter) syslogFilterClause {
	return syslogFilterClause{
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
	}
}

func netlogClause(f model.NetlogFilter) syslogFilterClause {
	return syslogFilterClause{
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
	}
}

// applySrvlogFilter applies f and then each of its exclusions. An exclusion's
// own Exclude is ignored.
func applySrvlogFilter(qb sq.SelectBuilder, f model.SrvlogFilter) sq.SelectBuilder {
	qb = applySyslogFilter(qb, srvlogClause(f))
	for _, ex := range f.Exclude {
		qb = excludeSyslog(qb, srvlogClause(ex))
	}
	return qb
}

// applyNetlogFilter applies f and then each of its exclusions. An exclusion's
// own Exclude is ignored.
func applyNetlogFilter(qb sq.SelectBuilder, f model.NetlogFilter) sq.SelectBuilder {
	qb = applySyslogFilter(qb, netlogClause(f))
	for _, ex := range f.Exclude {
		qb = excludeSyslog(qb, netlogClause(ex))
	}
	return qb
}
