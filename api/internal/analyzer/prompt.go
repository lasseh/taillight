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
type promptData struct {
	analysisData
	FeedDescription string
	FeedTitle       string
}

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
	return sysBuf.String(), userBuf.String(), nil
}
