package tui

import "fmt"

// humorCategory buckets the flavor-phrase banks by what the agent is
// currently doing, so the phrase matches the action instead of showing a
// generic "thinking" line while a shell command runs, or a combat phrase
// while it's just writing a memory note. Phrases cycle at roughly 20-second
// intervals so the indicator stays lively across long turns without
// flashing too quickly.
type humorCategory int

const (
	catThinking humorCategory = iota // model reasoning, no tool call in flight
	catRead                          // inspecting: read_file, grep, glob, lsp lookups, etc.
	catWrite                         // creating/editing: write_file, edit_file, git_commit, etc.
	catExecute                       // running something: shell, security_scan, latex_build, etc.
	catNetwork                       // reaching outside the workspace: web_fetch, git_pr, etc.
	catSpawn                         // delegating: agent
)

// toolCategory maps a builtin tool name to its flavor-phrase bucket.
var toolCategory = map[string]humorCategory{
	// Read / investigate
	"read_file": catRead, "ls": catRead, "glob": catRead, "grep": catRead,
	"definition": catRead, "hover": catRead, "document_symbols": catRead,
	"workspace_symbols": catRead, "call_hierarchy": catRead, "diagnostics": catRead,
	"references": catRead, "project_knowledge": catRead, "entity_recall": catRead,
	"ask_user": catRead, "skill": catRead, "tool_search": catRead,
	"task_list": catRead, "task_get": catRead, "task_output": catRead,
	"team_task_list": catRead, "team_inbox": catRead, "todo_list": catRead,
	"git": catRead, "cron_list": catRead,

	// Write / create
	"write_file": catWrite, "edit_file": catWrite, "multi_edit": catWrite,
	"git_commit": catWrite, "render_diagram": catWrite, "remember": catWrite,
	"save_skill": catWrite, "entity_remember": catWrite, "todo_add": catWrite,
	"todo_update": catWrite, "team_task_add": catWrite, "team_task_claim": catWrite,
	"team_task_complete": catWrite, "team_send": catWrite, "cron_delete": catWrite,
	"cron_toggle": catWrite, "latex_new_document": catWrite, "task_update": catWrite,

	// Execute
	"shell": catExecute, "security_scan": catExecute, "latex_build": catExecute,
	"cron_create": catExecute, "task_create": catExecute, "task_stop": catExecute,

	// Network
	"web_fetch": catNetwork, "web_search": catNetwork, "git_pr": catNetwork,
	"list_models": catNetwork,

	// Spawn
	"agent": catSpawn,
}

// categoryFor returns the flavor bucket for a tool name, defaulting to
// catExecute for anything unrecognized (MCP/plugin tools) — the same
// most-restrictive-assumption precedent used for MCP capability resolution
// (internal/mcp/tool.go: resolveCapability), since an unfamiliar action is
// safer described generically as "doing something" than implied read-only.
func categoryFor(toolName string) humorCategory {
	if c, ok := toolCategory[toolName]; ok {
		return c
	}
	return catExecute
}

// thinkingPhrases: model reasoning, no tool call in flight. Wizardly /
// intellectual flavor — consulting knowledge, not yet acting on it.
var thinkingPhrases = []string{
	"Consulting the Tome",
	"Deciphering Runes",
	"Casting Identify",
	"Communing with the Oracle",
	"Reading the Augury",
	"Attuning to the Artifact",
	"Seeking Arcane Insight",
	"Consulting the Stars",
	"Translating the Ancient Glyphs",
	"Meditating on the Arcane",
	"Discerning the Pattern",
	"Consulting the Dungeon Master",
	"Making a Wisdom Save",
	"Weighing the Prophecy",
}

// readPhrases: inspecting/searching tools. Investigation / discovery flavor.
var readPhrases = []string{
	"Studying the Ancient Map",
	"Decoding the Cipher",
	"Checking for Secret Doors",
	"Examining the Clue",
	"Casting Legend Lore",
	"Tracking the Quarry",
	"Following the Rumour",
	"Questioning the Innkeeper",
	"Scouting the Dungeon",
	"Checking the Monster Manual",
	"Rolling Perception",
	"Rolling Investigation",
	"Reading the Ledger",
	"Consulting the Bestiary",
}

// writePhrases: creating/editing/persisting tools. Crafting / preparation flavor.
var writePhrases = []string{
	"Brewing a Potion",
	"Mixing the Components",
	"Preparing the Ritual",
	"Weaving the Spell",
	"Firing the Kiln",
	"Inscribing the Scroll",
	"Sharpening the Blade",
	"Polishing the Armor",
	"Sorting the Loot",
	"Updating the Quest Log",
	"Spending the Gold",
	"Counting the Spoils",
	"Etching the Rune",
	"Forging the Enchantment",
}

// executePhrases: running shell/build/scan tools. Combat / action flavor.
var executePhrases = []string{
	"Rolling for Initiative",
	"Checking the Traps",
	"Preparing the Ambush",
	"Nocking the Arrow",
	"Eyeing the Critical Hit",
	"Attempting a Natural 20",
	"Spending a Spell Slot",
	"Springing the Trap",
	"Charging the Breach",
	"Calling for a Saving Throw",
	"Unleashing the Fireball",
	"Rolling Damage",
}

// networkPhrases: reaching outside the workspace. Travel / diplomacy flavor.
var networkPhrases = []string{
	"Sending a Raven",
	"Asking for Directions in the Underdark",
	"Negotiating with the Dragon",
	"Parleying with the Merchant",
	"Scrying Across the Realm",
	"Reading the Windswept Rumours",
	"Consulting the Cartographer's Guild",
	"Bartering at the Market",
	"Hailing a Passing Caravan",
	"Petitioning the Guildhall",
}

// spawnPhrases: delegating to sub-agents. Party / company flavor.
var spawnPhrases = []string{
	"Rallying the Party",
	"Arguing with the Party",
	"Summoning Reinforcements",
	"Assembling the Company",
	"Recruiting Adventurers",
	"Dispatching the Scouts",
	"Briefing the Henchmen",
	"Splitting the Party (Wisely)",
}

// phraseBank returns the phrase bank for a flavor bucket.
func phraseBank(cat humorCategory) []string {
	switch cat {
	case catRead:
		return readPhrases
	case catWrite:
		return writePhrases
	case catExecute:
		return executePhrases
	case catNetwork:
		return networkPhrases
	case catSpawn:
		return spawnPhrases
	default:
		return thinkingPhrases
	}
}

// plainPhrases are used when humor mode is off — straightforward status text.
var plainPhrases = []string{
	"thinking…",
	"working…",
	"processing…",
	"reasoning…",
}

// phraseChangeTicks is how many animation steps a phrase stays visible.
// At ~100 ms per tick this is roughly 20 seconds per phrase.
const phraseChangeTicks = 200

// thinkingPhrase returns the display phrase for the current animation step
// and flavor bucket. cat is ignored when humorMode is false.
func thinkingPhrase(animStep int, humorMode bool, cat humorCategory) string {
	if !humorMode {
		idx := (animStep / phraseChangeTicks) % len(plainPhrases)
		return plainPhrases[idx]
	}
	bank := phraseBank(cat)
	idx := (animStep / phraseChangeTicks) % len(bank)
	return bank[idx]
}

// formatStreamHint builds the dim suffix shown next to the spinning indicator:
//
//	· 5s · ↑4.2k ↓~38
//
// elapsedSecs < 1 suppresses the time segment. inputToks 0 suppresses tokens.
func formatStreamHint(elapsedSecs int, inputToks, liveTextBytes int) string {
	var s string
	if elapsedSecs > 0 {
		s += fmt.Sprintf(" · %ds", elapsedSecs)
	}
	if inputToks > 0 {
		s += " · ↑" + abbrevToks(inputToks)
	}
	if liveTextBytes > 0 {
		est := liveTextBytes / 4
		if est > 0 {
			s += " ↓~" + abbrevToks(est)
		}
	}
	return s
}

// abbrevToks formats a token count compactly: 1200 → "1.2k", 45 → "45".
func abbrevToks(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
