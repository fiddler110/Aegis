package builtin

import (
	"fmt"
	"strings"
)

// latexLogSummary is parsed from the compiler's stdout/stderr.
type latexLogSummary struct {
	success  bool
	errors   []string
	warnings []string
	pdfPath  string
	pages    int
	bibNote  string // one line about the bibliography pass, if any
}

// latexMaxWarnings caps how many distinct warnings are reported before the
// remainder is collapsed into a single "… and N more" line.
const latexMaxWarnings = 15

// parseLatexLog extracts errors, warnings, page count, and success status from
// a raw LaTeX compiler log. Deduplicates repeated warnings and caps them at
// latexMaxWarnings.
func parseLatexLog(log, pdfPath string, checkOnly bool) latexLogSummary {
	s := latexLogSummary{pdfPath: pdfPath}
	seen := make(map[string]bool)
	dropped := 0
	lines := strings.Split(log, "\n")

	for i, line := range lines {
		tr := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(tr, "! "):
			// LaTeX error — grab the next non-trivial line for context.
			msg := strings.TrimPrefix(tr, "! ")
			if i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				if next != "" && !strings.HasPrefix(next, "l.") && len(next) < 120 {
					msg += "  →  " + next
				}
			}
			if !seen[msg] {
				seen[msg] = true
				s.errors = append(s.errors, msg)
			}

		case strings.Contains(tr, "Warning:") && !strings.HasPrefix(tr, "%"):
			if seen[tr] {
				continue
			}
			seen[tr] = true
			// Count what the cap excludes as it happens, rather than
			// re-deriving it afterwards from a length that the "… and N more"
			// line itself perturbs.
			if len(s.warnings) < latexMaxWarnings {
				s.warnings = append(s.warnings, tr)
			} else {
				dropped++
			}

		case strings.HasPrefix(tr, "Output written on"):
			s.success = true
			// "Output written on foo.pdf (3 pages, 123 bytes)."
			if idx := strings.Index(tr, "("); idx >= 0 {
				fmt.Sscanf(tr[idx+1:], "%d pages", &s.pages)
			}
		}
	}

	// In check_only mode the compiler doesn't write "Output written on",
	// so infer success from the absence of fatal errors.
	if checkOnly && !strings.Contains(log, "Emergency stop") &&
		!strings.Contains(log, "Fatal error") {
		s.success = len(s.errors) == 0
	}

	if dropped > 0 {
		s.warnings = append(s.warnings, fmt.Sprintf("… and %d more warnings (see .log file)", dropped))
	}
	return s
}

func formatBuildResult(s latexLogSummary, compiler string, runs int) string {
	var b strings.Builder
	if s.success {
		fmt.Fprintf(&b, "BUILD SUCCESS  (%s, %d pass(es))\n", compiler, runs)
		if s.pages > 0 {
			fmt.Fprintf(&b, "Output: %s  (%d pages)\n", s.pdfPath, s.pages)
		} else {
			fmt.Fprintf(&b, "Output: %s\n", s.pdfPath)
		}
	} else {
		fmt.Fprintf(&b, "BUILD FAILED  (%s, %d pass(es))\n", compiler, runs)
	}
	if s.bibNote != "" {
		fmt.Fprintf(&b, "%s\n", s.bibNote)
	}

	if len(s.errors) > 0 {
		fmt.Fprintf(&b, "\n%d error(s):\n", len(s.errors))
		for i, e := range s.errors {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, e)
		}
	}
	if len(s.warnings) > 0 {
		fmt.Fprintf(&b, "\n%d warning(s):\n", len(s.warnings))
		for _, w := range s.warnings {
			fmt.Fprintf(&b, "  · %s\n", w)
		}
	}
	if s.success && len(s.errors) == 0 && len(s.warnings) == 0 {
		b.WriteString("Clean build — no errors or warnings.\n")
	}
	return b.String()
}
