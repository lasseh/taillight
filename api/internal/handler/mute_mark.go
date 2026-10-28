package handler

import (
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
)

// MuteRuleSet gives the netlog and srvlog read handlers the active mute
// rules. *mute.Set satisfies it. Take one Active() snapshot per request so
// exclusion and marking agree.
type MuteRuleSet interface {
	Active() mute.Snapshot
}

// markNetlogs sets MutedBy on each event from snap.
func markNetlogs(snap mute.Snapshot, events []model.NetlogEvent) {
	for i := range events {
		events[i].MutedBy = snap.MatchNetlog(events[i])
	}
}

// markSrvlogs sets MutedBy on each event from snap.
func markSrvlogs(snap mute.Snapshot, events []model.SrvlogEvent) {
	for i := range events {
		events[i].MutedBy = snap.MatchSrvlog(events[i])
	}
}
