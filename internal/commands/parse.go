package commands

import "strings"

// ParsedCommand is the result of parsing a slash command input.
type ParsedCommand struct {
	Name string   // command name without the leading slash, lowercased
	Args []string // positional arguments after the name
	Raw  string   // the original input string
}

// Parse extracts a slash command from user input. Returns nil if the
// input does not start with "/".
func Parse(input string) *ParsedCommand {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return nil
	}
	trimmed := input[1:]
	if trimmed == "" {
		return nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return nil
	}
	return &ParsedCommand{
		Name: strings.ToLower(fields[0]),
		Args: fields[1:],
		Raw:  input,
	}
}
