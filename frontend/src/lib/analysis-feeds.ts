import type { AnalysisFeed, AnalysisFrequency, AnalysisPromptMode } from '@/types/analysis'

// One table of what each analysis feed supports: its label and badge, which
// scope field it takes, and which prompt modes it accepts; the schedule
// cadences follow from the modes. It mirrors model.AnalysisFeedSpec on the backend, which stays the
// authority (the API rejects what a feed does not take); this table only
// keeps the UI from offering it. Keyed by the AnalysisFeed union, so a new
// feed value fails the build until its row exists.

export type AnalysisScopeKind = 'hosts' | 'services'

export interface AnalysisFeedSpec {
  label: string
  badgeClass: string
  scopeKind: AnalysisScopeKind
  modes: readonly AnalysisPromptMode[]
}

const SYSLOG_MODES: readonly AnalysisPromptMode[] = ['daily', 'weekly', 'incident']

export const ANALYSIS_FEEDS: Record<AnalysisFeed, AnalysisFeedSpec> = {
  netlog: {
    label: 'Netlog',
    badgeClass: 'bg-t-blue/10 text-t-blue',
    scopeKind: 'hosts',
    modes: SYSLOG_MODES,
  },
  srvlog: {
    label: 'Srvlog',
    badgeClass: 'bg-t-green/10 text-t-green',
    scopeKind: 'hosts',
    modes: SYSLOG_MODES,
  },
  applog: {
    label: 'Applog',
    badgeClass: 'bg-t-purple/10 text-t-purple',
    scopeKind: 'services',
    modes: ['daily', 'incident'],
  },
}

// Picker order.
export const ANALYSIS_FEED_ORDER: readonly AnalysisFeed[] = ['netlog', 'srvlog', 'applog']

export const feedOptions: readonly { value: AnalysisFeed; label: string }[] =
  ANALYSIS_FEED_ORDER.map((value) => ({ value, label: ANALYSIS_FEEDS[value].label }))

// feedSpec returns undefined for a feed value this build does not know (an
// API newer than the UI), so display code keeps a fallback.
export function feedSpec(feed: string): AnalysisFeedSpec | undefined {
  return feed in ANALYSIS_FEEDS ? ANALYSIS_FEEDS[feed as AnalysisFeed] : undefined
}

export function feedScopeKind(feed: AnalysisFeed): AnalysisScopeKind {
  return ANALYSIS_FEEDS[feed].scopeKind
}

// feedScopeNoun is the singular noun the scope picker uses for the feed.
export function feedScopeNoun(feed: AnalysisFeed): 'host' | 'service' {
  return feedScopeKind(feed) === 'services' ? 'service' : 'host'
}

export function feedAllowsMode(feed: AnalysisFeed, mode: AnalysisPromptMode): boolean {
  return ANALYSIS_FEEDS[feed].modes.includes(mode)
}

// The prompt mode a schedule cadence runs, as model.AnalysisModeForFrequency
// maps it: weekly and monthly both reuse the weekly prompt.
const FREQUENCY_MODE: Record<AnalysisFrequency, AnalysisPromptMode> = {
  daily: 'daily',
  weekly: 'weekly',
  monthly: 'weekly',
}

// feedFrequencies lists the cadences whose prompt mode the feed has, in
// display order.
export function feedFrequencies(feed: AnalysisFeed): AnalysisFrequency[] {
  return (Object.keys(FREQUENCY_MODE) as AnalysisFrequency[]).filter((f) =>
    feedAllowsMode(feed, FREQUENCY_MODE[f]),
  )
}

export function feedAllowsFrequency(feed: AnalysisFeed, frequency: AnalysisFrequency): boolean {
  return feedAllowsMode(feed, FREQUENCY_MODE[frequency])
}
