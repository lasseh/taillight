/** Rank ordering for applog levels — lower number = more severe. */
export const LEVEL_RANK: Record<string, number> = {
  FATAL: 0,
  ERROR: 1,
  WARN: 2,
  INFO: 3,
  DEBUG: 4,
}

/** Tailwind text color classes keyed by log level. */
export const levelColorClass: Record<string, string> = {
  FATAL: 'text-sev-emerg',
  ERROR: 'text-sev-alert',
  WARN: 'text-sev-crit',
  INFO: 'text-sev-notice',
  DEBUG: 'text-sev-debug',
}

/** Border color classes for the detail panel left accent. */
export const levelBorderClass: Record<string, string> = {
  FATAL: 'border-sev-emerg',
  ERROR: 'border-sev-alert',
  WARN: 'border-sev-crit',
  INFO: 'border-sev-notice',
  DEBUG: 'border-sev-debug',
}

/** Background color classes keyed by log level. */
export const levelBgColorClass: Record<string, string> = {
  FATAL: 'bg-sev-emerg',
  ERROR: 'bg-sev-alert',
  WARN: 'bg-sev-crit',
  INFO: 'bg-sev-notice',
  DEBUG: 'bg-sev-debug',
}

/** Background tint classes for high-severity rows. */
export const levelBgClass: Record<string, string> = {
  FATAL: 'bg-sev-bg-alert',
  ERROR: 'bg-sev-bg-alert',
  WARN: '',
}

/** CSS variable names for level colors, used by SeverityTimeline chart. */
export const levelColorVar: Record<string, string> = {
  FATAL: '--color-sev-emerg',
  ERROR: '--color-sev-alert',
  WARN: '--color-sev-crit',
  INFO: '--color-sev-notice',
  DEBUG: '--color-sev-debug',
}

/** Level options for the filter dropdown (minimum level). */
export const levelOptions = [
  { value: 'FATAL', label: 'FATAL', colorClass: 'text-sev-emerg' },
  { value: 'ERROR', label: 'ERROR', colorClass: 'text-sev-alert' },
  { value: 'WARN', label: 'WARN', colorClass: 'text-sev-crit' },
  { value: 'INFO', label: 'INFO', colorClass: 'text-sev-notice' },
  { value: 'DEBUG', label: 'DEBUG', colorClass: 'text-sev-debug' },
]
