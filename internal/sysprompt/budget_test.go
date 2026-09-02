package sysprompt

import (
	"strings"
	"testing"
)

// TestFitsLocalBudget_DefaultProfileAlwaysFits mirrors RepoMapFits/
// ContextFilesBudget: the default (non-local) profile has no prompt budget to
// protect, so an oversized suffix is still accepted there.
func TestFitsLocalBudget_DefaultProfileAlwaysFits(t *testing.T) {
	huge := strings.Repeat("word ", 10000)
	if !FitsLocalBudget(huge, false) {
		t.Error("default profile should accept any suffix size")
	}
}

func TestFitsLocalBudget_EmptySuffixAlwaysFits(t *testing.T) {
	if !FitsLocalBudget("", true) {
		t.Error("an empty suffix should always fit")
	}
}

func TestFitsLocalBudget_LocalProfileRejectsOversizedSuffix(t *testing.T) {
	huge := strings.Repeat("word ", 1000)
	if FitsLocalBudget(huge, true) {
		t.Error("a suffix well over LocalPromptSuffixMaxTokens should not fit under the local profile")
	}
}

func TestFitsLocalBudget_LocalProfileAcceptsShortSuffix(t *testing.T) {
	short := "This model expects snake_case tool arguments."
	if !FitsLocalBudget(short, true) {
		t.Error("a short quirk-note suffix should fit under the local profile")
	}
}
