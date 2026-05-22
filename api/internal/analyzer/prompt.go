package analyzer

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/lasseh/taillight/internal/model"
)

// Embedded default prompts. The directory tree is:
//
//	prompts/<mode>/system.md
//	prompts/<mode>/user.md
//
// where <mode> is one of "daily", "weekly", or "incident". Override by setting
// analysis.prompts_dir in config.yml to a directory with the same layout
// (<dir>/<mode>/system.md and <dir>/<mode>/user.md). Files are reloaded on
// every analysis run, so prompt edits take effect without a rebuild or restart.
//
//go:embed prompts
var embeddedPrompts embed.FS

const (
	systemPromptFile = "system.md"
	userPromptFile   = "user.md"

	// Prompt modes. Daily is the default; weekly is for trend reviews;
	// incident is for narrow-window manual triage.
	modeDaily    = "daily"
	modeWeekly   = "weekly"
	modeIncident = "incident"

	// embedRoot is the top-level directory inside embedded prompts that
	// holds the per-mode subdirectories.
	embedRoot = "prompts"
)

// validModes enumerates the prompt modes accepted by buildPrompt.
var validModes = map[string]struct{}{
	modeDaily:    {},
	modeWeekly:   {},
	modeIncident: {},
}

// promptFuncs are the template functions available to both prompts.
// Registered fresh on every parse so hot-reload picks up new files.
var promptFuncs = template.FuncMap{
	"severityLabel": model.SeverityLabel,
	"join":          strings.Join,
	"truncate":      truncatePromptString,
	"truncateAll":   truncatePromptStrings,
}

// truncatePromptString returns s clipped to at most n runes, appending a
// single ellipsis rune when truncation occurred. Real RFC 5424 MSGIDs are
// short (~50 chars max) so a reasonable n leaves them intact while
// shortening long msg_pattern fallbacks that include ASIC SDK function
// signatures and stack traces.
func truncatePromptString(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// truncatePromptStrings applies truncatePromptString to every element of
// ss, returning a new slice. Used by templates that join lists of
// signatures into a single line — without per-element truncation, one
// long msg_pattern in the list can dominate the rendered line.
func truncatePromptStrings(ss []string, n int) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = truncatePromptString(s, n)
	}
	return out
}

// formatScopeLabel renders the scope's host list as a single line for the
// user prompt's `Scope:` header. Empty input returns "" so the template can
// guard on `IsScoped` instead of empty-string checks. The count suffix lets
// the model and a human reader confirm at a glance how many hosts the
// report covers without counting commas.
func formatScopeLabel(hosts []string) string {
	if len(hosts) == 0 {
		return ""
	}
	noun := "host"
	if len(hosts) > 1 {
		noun = "hosts"
	}
	return fmt.Sprintf("%s (%d %s)", strings.Join(hosts, ", "), len(hosts), noun)
}

// feedDescription returns a human-readable description of the feed for use in prompts.
func feedDescription(feed string) string {
	switch feed {
	case feedNetlog:
		return "network device syslog data (routers, switches, firewalls)"
	case feedSrvlog:
		return "server syslog data (Linux, Windows servers)"
	case feedAll:
		return "combined syslog data from both network devices and servers"
	default:
		return "syslog data"
	}
}

// feedTitle returns a short title for the feed.
func feedTitle(feed string) string {
	switch feed {
	case feedNetlog:
		return "Netlog"
	case feedSrvlog:
		return "Srvlog"
	case feedAll:
		return "All Feeds"
	default:
		return "Log"
	}
}

// promptData wraps analysisData with feed-specific template fields.
//
// IsScoped is true when the run was restricted to an explicit host list; it
// gates the `Scope:` line in the user prompt and the section-suppression
// {{if not .IsScoped}} blocks. Hosts is the sorted host set (already
// normalized upstream) so the template can render the names verbatim
// without a sort/dedup call in template land.
type promptData struct {
	analysisData
	FeedDescription string
	FeedTitle       string
	IsScoped        bool
	ScopeLabel      string // pre-formatted "edge01.lab, edge02.lab (2 hosts)".
}

// scopedGuardSystemPreamble is the invariant system-prompt block prepended
// to every scoped run. It lives in code (not in the prompts/<mode>/system.md
// files) so prompt authors cannot accidentally drop it on an edit, and so
// hot-reload of the templates never loses the anti-hallucination guard.
//
// The wording is deliberately specific: "do not speculate about other
// hosts" rather than "be careful," because vague guidance gets ignored.
const scopedGuardSystemPreamble = "# Scope restriction\n\n" +
	"This report is restricted to the specific hosts named in the user message's `Scope:` line. " +
	"Do not claim or speculate about activity on hosts outside that scope. " +
	"Do not use phrases like \"across the fleet\", \"the rest of the cluster\", or \"other hosts in the environment\" — " +
	"there are no other hosts in this report. " +
	"When a signal would normally compare these hosts to others, frame it as \"these hosts vs. their own 7-day baseline\" " +
	"(the baseline in the data block has already been filtered to the same hosts). " +
	"\"Top Error Hosts\" and \"Cross-Host Event Clusters\" sections are intentionally absent — do not invent them."

// loadPromptSource returns the raw template text for the given prompt file and
// mode. When dir is empty, the embedded default is read; otherwise the file is
// read from <dir>/<mode>/<file> on every call so edits take effect without
// restarting the server. Unknown modes return an error rather than silently
// falling back to a default.
func loadPromptSource(dir, mode, file string) (string, error) {
	if _, ok := validModes[mode]; !ok {
		return "", fmt.Errorf("unknown prompt mode %q (want one of: daily, weekly, incident)", mode)
	}
	if dir == "" {
		path := embedRoot + "/" + mode + "/" + file
		b, err := embeddedPrompts.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("load embedded prompt %s: %w", path, err)
		}
		return string(b), nil
	}
	path := filepath.Join(dir, mode, file)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("load prompt %s: %w", path, err)
	}
	return string(b), nil
}

// parsePrompt parses one template, registering the shared FuncMap.
func parsePrompt(name, src string) (*template.Template, error) {
	t, err := template.New(name).Funcs(promptFuncs).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse %s prompt: %w", name, err)
	}
	return t, nil
}

// buildPrompt loads, parses, and renders the system and user prompts for the
// given mode. If promptsDir is empty, the embedded defaults are used. An empty
// mode is treated as "daily" for backwards compatibility with existing callers.
func buildPrompt(data analysisData, promptsDir, mode string) (string, string, error) {
	if mode == "" {
		mode = modeDaily
	}

	pd := promptData{
		analysisData:    data,
		FeedDescription: feedDescription(data.Feed),
		FeedTitle:       feedTitle(data.Feed),
		IsScoped:        len(data.Hosts) > 0,
		ScopeLabel:      formatScopeLabel(data.Hosts),
	}

	sysSrc, err := loadPromptSource(promptsDir, mode, systemPromptFile)
	if err != nil {
		return "", "", err
	}
	sysTmpl, err := parsePrompt("system", sysSrc)
	if err != nil {
		return "", "", err
	}

	userSrc, err := loadPromptSource(promptsDir, mode, userPromptFile)
	if err != nil {
		return "", "", err
	}
	userTmpl, err := parsePrompt("user", userSrc)
	if err != nil {
		return "", "", err
	}

	var sysBuf bytes.Buffer
	if err := sysTmpl.Execute(&sysBuf, pd); err != nil {
		return "", "", fmt.Errorf("render system prompt: %w", err)
	}

	var userBuf bytes.Buffer
	if err := userTmpl.Execute(&userBuf, pd); err != nil {
		return "", "", fmt.Errorf("render user prompt: %w", err)
	}

	sys := sysBuf.String()
	if pd.IsScoped {
		// Prepended in code, not in the template, so a prompt edit can't
		// silently drop the anti-fleet-language guard. The two newlines
		// separate the preamble from the existing system prompt cleanly.
		sys = scopedGuardSystemPreamble + "\n\n" + sys
	}
	return sys, userBuf.String(), nil
}
