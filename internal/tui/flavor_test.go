package tui

import "testing"

func TestCategoryForKnownTools(t *testing.T) {
	cases := map[string]humorCategory{
		"read_file":   catRead,
		"grep":        catRead,
		"write_file":  catWrite,
		"git_commit":  catWrite,
		"shell":       catExecute,
		"latex_build": catExecute,
		"web_fetch":   catNetwork,
		"web_search":  catNetwork,
		"agent":       catSpawn,
	}
	for name, want := range cases {
		if got := categoryFor(name); got != want {
			t.Errorf("categoryFor(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestCategoryForUnknownToolDefaultsToExecute(t *testing.T) {
	if got := categoryFor("some_mcp_tool"); got != catExecute {
		t.Errorf("categoryFor(unknown) = %v, want catExecute", got)
	}
}

func TestPhraseBankNoOverlapAndNonEmpty(t *testing.T) {
	cats := []humorCategory{catThinking, catRead, catWrite, catExecute, catNetwork, catSpawn}
	seen := map[string]humorCategory{}
	for _, c := range cats {
		bank := phraseBank(c)
		if len(bank) == 0 {
			t.Errorf("phraseBank(%v) is empty", c)
		}
		for _, phrase := range bank {
			if prev, ok := seen[phrase]; ok {
				t.Errorf("phrase %q appears in both bucket %v and %v", phrase, prev, c)
			}
			seen[phrase] = c
		}
	}
}

func TestThinkingPhrasePlainModeIgnoresCategory(t *testing.T) {
	got := thinkingPhrase(0, false, catExecute)
	if got != plainPhrases[0] {
		t.Errorf("thinkingPhrase(plain) = %q, want %q", got, plainPhrases[0])
	}
}

func TestThinkingPhraseHumorModeUsesBucket(t *testing.T) {
	got := thinkingPhrase(0, true, catNetwork)
	if got != networkPhrases[0] {
		t.Errorf("thinkingPhrase(humor, network) = %q, want %q", got, networkPhrases[0])
	}
}
