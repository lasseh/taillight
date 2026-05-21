package analyzer

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/lasseh/taillight/internal/model"
)

// Embedded default prompts. Used when AnalysisConfig.PromptsDir is empty.
// Override by setting analysis.prompts_dir in config.yml to a directory
// containing system.md and user.md — files are reloaded on every analysis run,
// so prompt edits take effect without a rebuild or restart.
//
//go:embed prompts/system.md
var defaultSystemPrompt string

//go:embed prompts/user.md
var defaultUserPrompt string

const (
	systemPromptFile = "system.md"
	userPromptFile   = "user.md"
)

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

// loadPromptSource returns the raw template text for the given prompt file.
// When dir is empty, the embedded default is used; otherwise the file is read
// from disk on every call so edits take effect without restarting the server.
func loadPromptSource(dir, file, embedded string) (string, error) {
	if dir == "" {
		return embedded, nil
	}
	path := filepath.Join(dir, file)
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

// buildPrompt loads, parses, and renders the system and user prompts.
// If promptsDir is empty, the embedded defaults are used.
func buildPrompt(data analysisData, promptsDir string) (string, string, error) {
	pd := promptData{
		analysisData:    data,
		FeedDescription: feedDescription(data.Feed),
		FeedTitle:       feedTitle(data.Feed),
	}

	sysSrc, err := loadPromptSource(promptsDir, systemPromptFile, defaultSystemPrompt)
	if err != nil {
		return "", "", err
	}
	sysTmpl, err := parsePrompt("system", sysSrc)
	if err != nil {
		return "", "", err
	}

	userSrc, err := loadPromptSource(promptsDir, userPromptFile, defaultUserPrompt)
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
