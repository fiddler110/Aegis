package tui

import (
	"os"
	"runtime"
	"strings"
)

// --- welcome content ---

func buildWelcomeContent(cfg Config, workDir string, th theme) string {
	username := getUsername()
	shortCWD := shortenPath(workDir)

	info := []string{
		"",
		th.titleMeta.Render("AI agent harness"),
		"",
		"Welcome back, " + th.welcomeName.Render(username) + "!",
		"",
		th.welcomeKey.Render("Model  ") + th.welcomeVal.Render(cfg.Model),
		th.welcomeKey.Render("Mode   ") + th.welcomeVal.Render(cfg.Mode),
		th.welcomeKey.Render("Dir    ") + th.cwdStyle.Render(shortCWD),
		"",
	}

	shield := renderAegisLogo()
	var b strings.Builder
	b.WriteString("\n")
	for i, shieldLine := range shield {
		b.WriteString(shieldLine)
		b.WriteString("  ")
		if i < len(info) {
			b.WriteString(info[i])
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(th.welcomeKey.Render("  ") +
		th.welcomeName.Render("/help") + th.welcomeKey.Render(" commands · ") +
		th.welcomeName.Render("ctrl+k") + th.welcomeKey.Render(" palette · ") +
		th.welcomeName.Render("shift+tab") + th.welcomeKey.Render(" mode · ") +
		th.welcomeName.Render("esc esc") + th.welcomeKey.Render(" undo"))
	b.WriteString("\n\n")
	return b.String()
}

func getUsername() string {
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "there"
}

func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	// Windows paths are case-insensitive; compare lowercased to avoid misses.
	homeCmp, pathCmp := home, path
	if runtime.GOOS == "windows" {
		homeCmp = strings.ToLower(home)
		pathCmp = strings.ToLower(path)
	}
	if strings.HasPrefix(pathCmp, homeCmp) {
		return "~" + path[len(home):]
	}
	return path
}
