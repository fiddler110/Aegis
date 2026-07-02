package tui

import "fmt"

// humorPhrases is the D&D-themed phrase bank shown while the model is
// generating. Phrases cycle at roughly 3-second intervals so the indicator
// stays lively across long turns without flashing too quickly.
var humorPhrases = []string{
	// 🧙 Wizardly / Intellectual
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
	"Brewing a Potion",
	"Mixing the Components",
	"Preparing the Ritual",
	"Weaving the Spell",
	"Firing the Kiln",

	// ⚔️ Combat / Action
	"Rolling for Initiative",
	"Sharpening the Blade",
	"Checking the Traps",
	"Scouting the Dungeon",
	"Looting the Goblin",
	"Polishing the Armor",
	"Preparing the Ambush",
	"Counting the Spoils",
	"Nocking the Arrow",
	"Eyeing the Critical Hit",

	// 🗺️ Investigation / Discovery
	"Tracking the Quarry",
	"Following the Rumour",
	"Questioning the Innkeeper",
	"Studying the Ancient Map",
	"Decoding the Cipher",
	"Checking for Secret Doors",
	"Examining the Clue",
	"Casting Legend Lore",
	"Discerning the Pattern",

	// 🎲 Classic D&D moments
	"Asking for Directions in the Underdark",
	"Arguing with the Party",
	"Updating the Quest Log",
	"Resting at the Inn",
	"Negotiating with the Dragon",
	"Sorting the Loot",
	"Spending the Gold",
	"Rolling Perception",
	"Consulting the Dungeon Master",
	"Mapping the Dungeon",
	"Making a Wisdom Save",
	"Attempting a Natural 20",
	"Spending a Spell Slot",
	"Rolling Investigation",
	"Checking the Monster Manual",
}

// plainPhrases are used when humor mode is off — straightforward status text.
var plainPhrases = []string{
	"thinking…",
	"working…",
	"processing…",
	"reasoning…",
}

// phraseChangeTicks is how many animation steps a phrase stays visible.
// At ~100 ms per tick this is roughly 3 seconds per phrase.
const phraseChangeTicks = 30

// thinkingPhrase returns the display phrase for the current animation step.
func thinkingPhrase(animStep int, humorMode bool) string {
	bank := humorPhrases
	if !humorMode {
		bank = plainPhrases
	}
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
