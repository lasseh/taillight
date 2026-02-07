export const severityLabels: Record<number, string> = {
  0: 'emerg',
  1: 'alert',
  2: 'crit',
  3: 'err',
  4: 'warning',
  5: 'notice',
  6: 'info',
  7: 'debug',
}

export const facilityLabels: Record<number, string> = {
  0: 'kern',
  1: 'user',
  2: 'mail',
  3: 'daemon',
  4: 'auth',
  5: 'syslog',
  6: 'lpr',
  7: 'news',
  8: 'uucp',
  9: 'cron',
  10: 'authpriv',
  11: 'ftp',
  12: 'ntp',
  13: 'security',
  14: 'console',
  15: 'clock',
  16: 'local0',
  17: 'local1',
  18: 'local2',
  19: 'local3',
  20: 'local4',
  21: 'local5',
  22: 'local6',
  23: 'local7',
}

/** Tailwind text color classes keyed by severity level. */
export const severityColorClass: Record<number, string> = {
  0: 'text-sev-emerg',
  1: 'text-sev-alert',
  2: 'text-sev-crit',
  3: 'text-sev-err',
  4: 'text-sev-warning',
  5: 'text-sev-notice',
  6: 'text-sev-info',
  7: 'text-sev-debug',
}

/** Border color classes for the detail panel left accent. */
export const severityBorderClass: Record<number, string> = {
  0: 'border-sev-emerg',
  1: 'border-sev-alert',
  2: 'border-sev-crit',
  3: 'border-sev-err',
  4: 'border-sev-warning',
  5: 'border-sev-notice',
  6: 'border-sev-info',
  7: 'border-sev-debug',
}

/** Background tint classes for high-severity rows. */
export const severityBgClass: Record<number, string> = {
  0: 'bg-sev-bg-alert',
  1: 'bg-sev-bg-alert',
  2: 'bg-sev-bg-crit',
  3: 'bg-sev-bg-err',
}

/** Text color classes keyed by severity label (e.g. "emerg", "alert"). */
export const severityColorClassByLabel: Record<string, string> = {
  emerg: 'text-sev-emerg',
  alert: 'text-sev-alert',
  crit: 'text-sev-crit',
  err: 'text-sev-err',
  warning: 'text-sev-warning',
  notice: 'text-sev-notice',
  info: 'text-sev-info',
  debug: 'text-sev-debug',
}

/** Background color classes keyed by severity label. */
export const severityBgClassByLabel: Record<string, string> = {
  emerg: 'bg-sev-emerg',
  alert: 'bg-sev-alert',
  crit: 'bg-sev-crit',
  err: 'bg-sev-err',
  warning: 'bg-sev-warning',
  notice: 'bg-sev-notice',
  info: 'bg-sev-info',
  debug: 'bg-sev-debug',
}

/** Severity options for the filter dropdown (severity_max). */
export const severityOptions = [
  { value: '0', label: 'EMERG' },
  { value: '1', label: 'ALERT' },
  { value: '2', label: 'CRIT' },
  { value: '3', label: 'ERR' },
  { value: '4', label: 'WARNING' },
  { value: '5', label: 'NOTICE' },
  { value: '6', label: 'INFO' },
  { value: '7', label: 'DEBUG' },
]
