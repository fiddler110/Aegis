package builtin

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/tool"
)

// P67.8 — per-command flag configuration replaces the per-binary allowlist.
//
// The old table allowlisted whole *binaries*: if the argv0 was on the list,
// every flag it could carry came with it. That was safe only for binaries with
// no file-writing form at all, so `sort`, `tree` and `uniq` were excluded
// outright ("no argument parsing makes them read-only") — and `rg`, `fd` and
// flag-carrying `gh` never got in. Argument parsing is what was missing, and
// this file now does it: each command carries a table of the flags it may
// use, each flag declares whether it takes no value / a string / a number
// (optionally constrained by a regex), each command says whether it honors
// POSIX `--`, how many positionals it may have, and may carry a predicate for
// the things flags cannot express. **An unlisted flag fails closed**: the
// command simply does not get the CapRead downgrade and runs under the normal
// CapExecute approval, exactly as before.
//
// Two rules govern every entry below, and neither is optional:
//
//  1. Flag parsing decides whether a command *can* be read-only. Path
//     confinement (argvStaysInRoot, argv_confine.go) decides whether *this
//     invocation* is. Both run, never either — and argvStaysInRoot confines
//     attached flag values too ("--file=/etc/passwd", "-o/etc/passwd"), which
//     is what VULN-02 rode in on.
//  2. "Does this flag write a file?" is not the whole question. A flag can be
//     unsafe for a reason its name does not suggest: fd's `--list-details`
//     internally execs `ls` (a PATH-hijack surface), sort's
//     `--compress-program` execs an arbitrary program on its temp files, rg's
//     `--pre` runs a preprocessor command, gh's `--web` launches a browser.
//     Every flag admitted below was asked that question; the ones that failed
//     it are named in the per-command comments so the reasoning is not lost.
//
// Widening this classification widens two things at once: what the engine will
// run in parallel without the exec lock, and what plan mode approves *without
// asking*. A false negative here costs a call its downgrade; a false positive
// auto-approves a mutation. When in doubt, leave the flag out.

// flagArg is the argument type a flag takes.
type flagArg int

const (
	noArg  flagArg = iota // "--long", "-l": the flag stands alone
	strArg                // "--glob=x", "--glob x", "-gx", "-g x"
	numArg                // same spellings, value must parse as a number
)

// flagSpec describes one permitted flag.
type flagSpec struct {
	arg flagArg
	// optional marks a flag whose value may be omitted ("--color" and
	// "--color=auto" are both valid). An optional value is only ever read
	// from the attached form; a bare "--color" never swallows the next token,
	// which would otherwise eat a path operand.
	optional bool
	// pattern, when non-nil, must match the flag's value. It is the escape
	// hatch for values that are neither a free string nor a plain number —
	// head/tail's "-c 10K" size syntax, for instance.
	pattern *regexp.Regexp
}

// anyPositionals is the maxPositionals value for a command whose positional
// arguments are all inputs. Commands whose *last* positional is an output
// (uniq) or an operand with side effects (hostname) set a finite number
// instead. maxPositionals is always written out explicitly: the zero value
// means "no positionals", which is the fail-closed direction.
const anyPositionals = -1

// commandSpec is the read-only configuration for one command.
type commandSpec struct {
	// capability is what a matching invocation is downgraded to. The zero
	// value means tool.CapRead. `gh` sets tool.CapNetwork instead — see the
	// gh entry.
	capability tool.Capability
	// flags is the complete set of permitted flags, keyed by their exact
	// spelling including dashes. Anything not in here fails closed.
	flags map[string]flagSpec
	// doubleDash: the command honors POSIX "--" as end-of-flags. A command
	// that does not (PowerShell cmdlets, `pwd`) rejects a "--" token as an
	// unlisted flag.
	doubleDash bool
	// singleDashLong: flags are whole words behind a single dash and are
	// matched case-insensitively, with an optional ":value" attachment. This
	// is PowerShell's parameter style; it also disables short-flag clustering,
	// so "-Recurse" is one flag and not eight.
	singleDashLong bool
	// numericShorthand: a bare "-20" is a count, not a flag cluster (head,
	// tail, uniq's obsolete forms).
	numericShorthand bool
	// maxPositionals bounds the non-flag arguments; anyPositionals for no
	// bound. This is the whole defense for `uniq`, whose second positional is
	// an OUTPUT file.
	maxPositionals int
	// subcommands, when non-nil, requires the first argument to name one of
	// them and hands the rest to it. Nested arbitrarily (gh pr view).
	subcommands map[string]*commandSpec
	// predicate is the last resort for a rule flags cannot express — see
	// `date`, whose *positional* is a set-the-system-clock operand on BSD.
	// It sees the positionals in order.
	predicate func(positionals []string) bool
}

// ---------------------------------------------------------------------------
// Table-construction helpers. flagsOf merges groups into one map; noneOf/
// strOf/numOf/optOf/patOf name the groups so the tables below read as
// "these flags take nothing, these take a string, ...".
// ---------------------------------------------------------------------------

type flagGroup struct {
	spec  flagSpec
	names []string
}

func noneOf(names ...string) flagGroup { return flagGroup{flagSpec{arg: noArg}, names} }
func strOf(names ...string) flagGroup  { return flagGroup{flagSpec{arg: strArg}, names} }
func numOf(names ...string) flagGroup  { return flagGroup{flagSpec{arg: numArg}, names} }
func optOf(names ...string) flagGroup {
	return flagGroup{flagSpec{arg: strArg, optional: true}, names}
}
func patOf(re *regexp.Regexp, names ...string) flagGroup {
	return flagGroup{flagSpec{arg: strArg, pattern: re}, names}
}

func flagsOf(groups ...flagGroup) map[string]flagSpec {
	m := make(map[string]flagSpec)
	for _, g := range groups {
		for _, n := range g.names {
			m[n] = g.spec
		}
	}
	return m
}

// sizePattern is head/tail/du's size argument: a count with an optional
// SI/binary suffix ("512", "10K", "1MB", "+5"). Constraining it is not a
// safety property on its own — argvStaysInRoot would confine a path-shaped
// value anyway — it is what keeps a size flag from being a general string
// slot that a future reader mistakes for one.
var sizePattern = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?[bcwkKMGTPEZY]{0,2}B?$`)

// ---------------------------------------------------------------------------
// The command table.
// ---------------------------------------------------------------------------

// readOnlyShellCommands maps a lowercased binary name to its configuration.
// The binary name is matched after stripping a directory prefix and a Windows
// .exe/.cmd/.bat suffix, so "/bin/cat" and "Cat.exe" both land on "cat".
//
// ===========================================================================
// DELIBERATELY ABSENT, and why — these are NOT oversights and do not become
// admissible under flag parsing. The reasoning is the expensive part; keep it
// with the table.
//
//   - env, printenv (P40.1). Read-only in the filesystem sense, but they dump
//     the daemon's *process environment*, which holds the provider API keys
//     (config.loadDotEnv os.Setenv's .aegis/.env into the process,
//     ProviderAPIKey reads os.Getenv). A CapRead downgrade auto-approves them
//     under plan mode, leaking the keys into the transcript and the SQLite
//     session store before the CapNetwork egress gate ever fires. No flag
//     table changes this: the *default* output is the leak. Falling back to
//     the normal CapExecute approval is the safe posture, and they are low
//     value as read-only anyway.
//   - ps (P66.3/SEC-04). `ps auxwwe` prints the daemon's own process
//     environment — the same provider-key leak as env/printenv, reached by a
//     different binary. Excluding `e` from a flag table would not help: `ps`
//     accepts BSD-style flag clusters without dashes at all ("auxwwe"), so
//     the leak has no reliable flag spelling to deny.
//   - less, more (P66.3/SEC-04). Pagers that shell out: `!command` runs a
//     shell, `v` opens an editor. They are program launchers wearing a
//     reader's name, and no flag table can take that away.
//   - find. `-exec`/`-execdir` run arbitrary commands, `-delete` removes
//     files, `-fprintf`/`-fls` write files. The safe subset is real but the
//     unsafe primaries are *operands*, not flags, and find's operand grammar
//     is a whole expression language. `fd` covers the same ground with a flag
//     grammar this parser can actually check — use it instead.
//   - xargs, sh, bash, powershell, sudo, ssh, curl, wget, nc. Program
//     launchers or network clients; nothing to parse.
//
// ===========================================================================
var readOnlyShellCommands = map[string]*commandSpec{
	// --- Listing and reading -------------------------------------------------
	"ls": {
		// Nothing ls does writes. Its long-listing flags (-l, -o, -g) are
		// pure formatting — note that -o here means "long format, no group",
		// unrelated to sort's output-file -o. Per-command tables are exactly
		// what lets the same spelling be safe in one command and refused in
		// another.
		flags: flagsOf(
			noneOf("-a", "-A", "-b", "-c", "-C", "-d", "-f", "-F", "-g", "-G", "-h", "-H",
				"-i", "-k", "-l", "-L", "-m", "-n", "-N", "-o", "-p", "-q", "-Q", "-r", "-R",
				"-s", "-S", "-t", "-u", "-U", "-v", "-x", "-X", "-1",
				"--all", "--almost-all", "--escape", "--directory", "--classify",
				"--no-group", "--human-readable", "--si", "--dereference",
				"--inode", "--numeric-uid-gid", "--literal", "--quote-name",
				"--reverse", "--recursive", "--size", "--group-directories-first",
				"--author", "--full-time", "--dereference-command-line"),
			numOf("-w", "--width", "-T", "--tabsize"),
			optOf("--color", "--indicator-style", "--quoting-style", "--hyperlink"),
			strOf("--sort", "--time", "--time-style", "--format", "--block-size",
				"-I", "--ignore", "--hide"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"cat": {
		flags: flagsOf(
			noneOf("-A", "-b", "-e", "-E", "-n", "-s", "-t", "-T", "-u", "-v",
				"--show-all", "--number-nonblank", "--show-ends", "--number",
				"--squeeze-blank", "--show-tabs", "--show-nonprinting"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"head": {
		flags: flagsOf(
			noneOf("-q", "-v", "-z", "--quiet", "--silent", "--verbose", "--zero-terminated"),
			patOf(sizePattern, "-c", "--bytes", "-n", "--lines"),
		),
		doubleDash: true, numericShorthand: true, maxPositionals: anyPositionals,
	},
	"tail": {
		flags: flagsOf(
			noneOf("-f", "-F", "-q", "-v", "-z", "-r",
				"--quiet", "--silent", "--verbose", "--zero-terminated", "--retry"),
			patOf(sizePattern, "-c", "--bytes", "-n", "--lines"),
			numOf("-s", "--sleep-interval", "--pid", "--max-unchanged-stats"),
			optOf("--follow"),
		),
		doubleDash: true, numericShorthand: true, maxPositionals: anyPositionals,
	},
	"wc": {
		flags: flagsOf(
			noneOf("-c", "-l", "-L", "-m", "-w",
				"--bytes", "--chars", "--lines", "--max-line-length", "--words"),
			strOf("--files0-from"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"nl": {
		flags: flagsOf(
			noneOf("-p", "--no-renumber"),
			strOf("-b", "--body-numbering", "-d", "--section-delimiter",
				"-f", "--footer-numbering", "-h", "--header-numbering",
				"-n", "--number-format", "-s", "--number-separator"),
			numOf("-i", "--line-increment", "-l", "--join-blank-lines",
				"-v", "--starting-line-number", "-w", "--number-width"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"cut": {
		flags: flagsOf(
			noneOf("-n", "-s", "-z", "--complement", "--only-delimited", "--zero-terminated"),
			strOf("-b", "--bytes", "-c", "--characters", "-d", "--delimiter",
				"-f", "--fields", "--output-delimiter"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"tr": {
		// tr never opens a file: it filters stdin to stdout, so its
		// positionals are character SETs rather than paths. They are still fed
		// to argvStaysInRoot, which resolves a set like "a-z" as a relative
		// name inside the root and passes — the conservative direction.
		flags:      flagsOf(noneOf("-c", "-C", "-d", "-s", "-t", "-u", "--complement", "--delete", "--squeeze-repeats", "--truncate-set1")),
		doubleDash: true, maxPositionals: 2,
	},
	"sort": {
		// P67.8 admits sort, which the binary allowlist had to refuse whole.
		//
		// DENIED, and why:
		//   -o/--output writes the sorted output to a file — VULN-02's escape,
		//     in both its separated ("-o out") and attached ("-oout") forms.
		//   --compress-program=PROG execs an arbitrary program over sort's
		//     temporary files. Nothing in the name says "runs a binary"; this
		//     is the class of flag rule 2 above exists for.
		//   --random-source=FILE reads an arbitrary file as entropy. Path
		//     confinement would contain it, but a read-only classifier has no
		//     reason to want it.
		//   --debug is harmless but prints to stderr only; omitted to keep the
		//     surface minimal.
		flags: flagsOf(
			noneOf("-b", "-c", "-C", "-d", "-f", "-g", "-h", "-i", "-M", "-m", "-n",
				"-R", "-r", "-s", "-u", "-V", "-z",
				"--ignore-leading-blanks", "--dictionary-order", "--ignore-case",
				"--general-numeric-sort", "--human-numeric-sort", "--ignore-nonprinting",
				"--month-sort", "--numeric-sort", "--random-sort", "--reverse",
				"--sort", "--stable", "--merge", "--unique", "--version-sort",
				"--zero-terminated"),
			optOf("--check"),
			strOf("-k", "--key", "-t", "--field-separator", "-T", "--temporary-directory",
				"-S", "--buffer-size", "--files0-from"),
			numOf("--parallel"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"uniq": {
		// P67.8 admits uniq. Its file-writing form is not a flag at all:
		// `uniq INPUT OUTPUT` writes the second positional. maxPositionals: 1
		// is the entire defense, and it is why maxPositionals exists.
		flags: flagsOf(
			noneOf("-c", "-d", "-D", "-i", "-u", "-z",
				"--count", "--repeated", "--all-repeated", "--ignore-case",
				"--unique", "--zero-terminated"),
			numOf("-f", "--skip-fields", "-s", "--skip-chars", "-w", "--check-chars"),
			optOf("--group"),
		),
		doubleDash: true, numericShorthand: true, maxPositionals: 1,
	},
	"tree": {
		// P67.8 admits tree.
		//
		// DENIED: -o/--output writes the listing to a file (the VULN-02 shape
		// again), and --fromfile makes tree read a file listing instead of the
		// filesystem — harmless but a different program, so it fails closed.
		// tree's -X/-J/-H emit XML/JSON/HTML to *stdout*, not to a file, and
		// are admitted.
		flags: flagsOf(
			noneOf("-a", "-A", "-c", "-C", "-d", "-D", "-f", "-F", "-g", "-i", "-J",
				"-l", "-n", "-N", "-p", "-q", "-Q", "-r", "-R", "-s", "-S", "-t", "-u",
				"-U", "-v", "-x", "-X",
				"--noreport", "--dirsfirst", "--filesfirst", "--si", "--du", "--inodes",
				"--device", "--prune", "--matchdirs", "--nolinks", "--version",
				"--ignore-case"),
			numOf("-L", "--level", "--filelimit"),
			strOf("-P", "-I", "-H", "-T", "--charset", "--timefmt", "--sort"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},

	// --- Path and comparison utilities ---------------------------------------
	"diff": {
		// GNU diff has no writing form at all — unlike sort/tree it takes no
		// -o/--output; every mode (unified, context, ed-script, side-by-side)
		// only ever prints to stdout. -D/--ifdef and -F/--show-function-line
		// take a string it embeds in the output, not a path it opens for
		// writing. Long numeric spellings (--unified=N, --context=N) are left
		// out for parsing simplicity — an omitted flag just costs the call its
		// downgrade, the safe direction — but the short forms (-U, -C) cover
		// the common case.
		flags: flagsOf(
			noneOf("-a", "-b", "-B", "-c", "-d", "-e", "-i", "-N", "-p", "-q", "-r",
				"-s", "-t", "-T", "-u", "-w", "-y",
				"--text", "--brief", "--recursive", "--new-file", "--ignore-case",
				"--ignore-blank-lines", "--ignore-space-change", "--ignore-all-space",
				"--report-identical-files", "--side-by-side", "--expand-tabs",
				"--initial-tab", "--show-c-function", "--strip-trailing-cr"),
			numOf("-C", "-U", "--horizon-lines", "-W", "--width"),
			strOf("-D", "--ifdef", "-F", "--show-function-line", "-I",
				"--ignore-matching-lines", "-S", "--starting-file",
				"-x", "--exclude", "-X", "--exclude-from"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"basename": {
		flags: flagsOf(
			noneOf("-a", "-z", "--multiple", "--zero"),
			strOf("-s", "--suffix"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"dirname": {
		flags:      flagsOf(noneOf("-z", "--zero")),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"readlink": {
		// All of -f/-e/-m's variants just change how far the symlink chain is
		// resolved before printing; none of them write anything.
		flags: flagsOf(
			noneOf("-e", "-f", "-m", "-n", "-q", "-s", "-v", "-z",
				"--canonicalize", "--canonicalize-existing", "--canonicalize-missing",
				"--no-newline", "--quiet", "--silent", "--verbose", "--zero"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},

	// --- Metadata ------------------------------------------------------------
	"stat": {
		flags: flagsOf(
			noneOf("-f", "-L", "-t", "-Z", "--dereference", "--file-system",
				"--terse", "--context"),
			strOf("-c", "--format", "--printf", "--cached"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"file": {
		// DENIED: -C/--compile. `file -C -m magicfile` *writes* a compiled
		// .mgc file next to the magic file — a writing form in a binary whose
		// entire purpose reads as read-only. Exactly rule 2.
		flags: flagsOf(
			noneOf("-b", "-h", "-i", "-k", "-L", "-n", "-N", "-p", "-r", "-s", "-v",
				"-z", "-0", "--brief", "--no-dereference", "--mime", "--mime-type",
				"--mime-encoding", "--keep-going", "--dereference", "--no-pad",
				"--raw", "--uncompress", "--print0", "--separator"),
			strOf("-e", "--exclude", "-F", "-f", "--files-from", "-m", "--magic-file"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"du": {
		flags: flagsOf(
			noneOf("-a", "-b", "-c", "-h", "-H", "-k", "-l", "-L", "-m", "-P", "-s",
				"-S", "-x", "-0", "--all", "--apparent-size", "--bytes", "--total",
				"--dereference", "--dereference-args", "--human-readable", "--si",
				"--one-file-system", "--separate-dirs", "--summarize", "--null",
				"--count-links", "--inodes"),
			numOf("-d", "--max-depth"),
			strOf("--exclude", "--exclude-from", "--files0-from", "--threshold"),
			patOf(sizePattern, "-B", "--block-size"),
			optOf("--time"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"df": {
		// df's --output=FIELD_LIST selects *columns*; it writes nothing. It is
		// admitted here while sort's --output is denied, which is the point of
		// per-command tables: a global flag denylist cannot tell them apart.
		flags: flagsOf(
			noneOf("-a", "-h", "-H", "-i", "-k", "-l", "-P", "-T", "-v",
				"--all", "--human-readable", "--si", "--inodes", "--local",
				"--portability", "--print-type", "--no-sync", "--sync", "--total"),
			strOf("-t", "--type", "-x", "--exclude-type"),
			patOf(sizePattern, "-B", "--block-size"),
			optOf("--output"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},

	// --- Host and identity ---------------------------------------------------
	"pwd":    {flags: flagsOf(noneOf("-L", "-P", "--logical", "--physical")), maxPositionals: 0},
	"whoami": {flags: flagsOf(), maxPositionals: 0},
	"id": {
		flags: flagsOf(noneOf("-a", "-g", "-G", "-n", "-r", "-u", "-z", "-Z",
			"--group", "--groups", "--name", "--real", "--user", "--zero", "--context")),
		maxPositionals: 1,
	},
	"uname": {
		flags: flagsOf(noneOf("-a", "-s", "-n", "-r", "-v", "-m", "-p", "-i", "-o",
			"--all", "--kernel-name", "--nodename", "--kernel-release", "--kernel-version",
			"--machine", "--processor", "--hardware-platform", "--operating-system")),
		maxPositionals: 0,
	},
	"hostname": {
		// maxPositionals: 0 is load-bearing. `hostname foo` SETS the system
		// hostname; the read-only form is the bare invocation. -F/--file has
		// the same effect from a file and is denied.
		flags: flagsOf(noneOf("-s", "-f", "-d", "-i", "-I", "-A", "-a", "-y",
			"--short", "--fqdn", "--long", "--domain", "--ip-address", "--all-ip-addresses",
			"--all-fqdns", "--alias", "--yp", "--nis")),
		maxPositionals: 0,
	},
	"date": {
		// DENIED: -s/--set writes the system clock. And on BSD/macOS `date`
		// the clock-setting form is a *positional* ("date 202601011200"),
		// which no flag table can express — hence the predicate: a positional
		// is only admissible as a "+FORMAT" output template.
		flags: flagsOf(
			noneOf("-u", "-R", "-L", "--utc", "--universal", "--rfc-email", "--debug"),
			strOf("-d", "--date", "-f", "--file", "-r", "--reference", "--rfc-3339"),
			optOf("-I", "--iso-8601"),
		),
		doubleDash: false, maxPositionals: 1,
		predicate: func(pos []string) bool {
			for _, p := range pos {
				if !strings.HasPrefix(p, "+") {
					return false
				}
			}
			return true
		},
	},
	"which": {
		flags:          flagsOf(noneOf("-a", "-s", "-v", "--all", "--skip-alias", "--skip-functions")),
		maxPositionals: anyPositionals,
	},
	"type": {
		flags:          flagsOf(noneOf("-a", "-f", "-p", "-P", "-t")),
		maxPositionals: anyPositionals,
	},

	// --- Search --------------------------------------------------------------
	"grep": {
		// grep's -o is --only-matching, not an output file. Same spelling,
		// opposite meaning from sort's; the per-command table is what lets
		// both be right.
		flags: flagsOf(
			noneOf("-a", "-b", "-c", "-E", "-F", "-G", "-H", "-h", "-i", "-I", "-l",
				"-L", "-n", "-o", "-P", "-q", "-r", "-R", "-s", "-U", "-v", "-w", "-x",
				"-y", "-z", "-Z",
				"--text", "--byte-offset", "--count", "--extended-regexp",
				"--fixed-strings", "--basic-regexp", "--perl-regexp", "--with-filename",
				"--no-filename", "--ignore-case", "--no-ignore-case", "--files-with-matches",
				"--files-without-match", "--line-number", "--only-matching", "--quiet",
				"--silent", "--recursive", "--dereference-recursive", "--no-messages",
				"--invert-match", "--word-regexp", "--line-regexp", "--null",
				"--null-data", "--line-buffered", "--initial-tab", "-T"),
			numOf("-A", "--after-context", "-B", "--before-context", "-C", "--context",
				"-m", "--max-count"),
			strOf("-e", "--regexp", "-f", "--file", "-d", "--directories",
				"-D", "--devices", "--include", "--exclude", "--exclude-dir",
				"--exclude-from", "--binary-files", "--label", "--group-separator"),
			optOf("--color", "--colour"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"rg": {
		// P67.8 opens up ripgrep with its flags rather than not at all.
		//
		// DENIED, and why:
		//   --pre=CMD runs an arbitrary preprocessor command over every file
		//     rg opens, and --pre-glob only scopes it. This is a program
		//     launcher spelled as a search flag.
		//   --hostname-bin=CMD names a binary rg execs to resolve the hostname
		//     for hyperlinks — arbitrary execution from a flag whose name
		//     suggests a lookup.
		//   -z/--search-zip decompresses via external decompressor binaries,
		//     a PATH-hijack surface of the same shape.
		//   --generate emits shell completions / man pages; harmless but it is
		//     not a search, so it fails closed.
		// rg's -r/--replace only rewrites the *printed* output; it never
		// touches the file, so it is admitted.
		flags: flagsOf(
			noneOf("-i", "-s", "-S", "-v", "-w", "-x", "-n", "-N", "-c", "-l", "-L",
				"-F", "-H", "-I", "-o", "-p", "-q", "-u", "-a", "-U", "-b", "-P", "-V", "-0",
				"--ignore-case", "--case-sensitive", "--smart-case", "--invert-match",
				"--word-regexp", "--line-regexp", "--line-number", "--no-line-number",
				"--count", "--count-matches", "--files-with-matches", "--files-without-match",
				"--fixed-strings", "--with-filename", "--no-filename", "--only-matching",
				"--pretty", "--quiet", "--text", "--multiline", "--multiline-dotall",
				"--hidden", "--no-hidden", "--no-ignore", "--no-ignore-vcs",
				"--no-ignore-parent", "--no-ignore-global", "--no-ignore-dot",
				"--no-ignore-files", "--no-messages", "--no-heading", "--heading",
				"--json", "--stats", "--files", "--null", "--vimgrep", "--column",
				"--no-config", "--one-file-system", "--crlf", "--binary", "--trim",
				"--block-buffered", "--line-buffered", "--byte-offset", "--follow",
				"--unrestricted", "--pcre2", "--no-unicode", "--include-zero",
				"--no-require-git", "--version", "--debug"),
			numOf("-m", "--max-count", "-A", "--after-context", "-B", "--before-context",
				"-C", "--context", "--max-depth", "-M", "--max-columns", "-j", "--threads"),
			strOf("-e", "--regexp", "-f", "--file", "-g", "--glob", "--iglob",
				"-t", "--type", "-T", "--type-not", "--type-add", "-r", "--replace",
				"--sort", "--sortr", "--color", "--colors", "--engine",
				"--context-separator", "--field-context-separator",
				"--field-match-separator", "--path-separator", "--ignore-file",
				"--max-filesize", "--encoding", "-E"),
			optOf("--max-columns-preview"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},
	"fd": {
		// P67.8 opens up fd.
		//
		// DENIED, and why:
		//   -x/--exec and -X/--exec-batch run a command per result. Obvious.
		//   -l/--list-details is the instructive one: it does not merely print
		//     more columns, it internally execs `ls` to render them. A flag
		//     whose name says "details" is a PATH-hijack surface. This is the
		//     example rule 2 was written from — never assume a flag's blast
		//     radius from its name.
		//   --gen-completions writes/emits shell completions; not a search.
		// fd's -o/--owner filters by owner and writes nothing — the third
		// distinct meaning of "-o" in this table.
		flags: flagsOf(
			noneOf("-H", "-I", "-s", "-i", "-g", "-F", "-a", "-L", "-p", "-0", "-u",
				"-q", "-h", "-V",
				"--hidden", "--no-hidden", "--no-ignore", "--no-ignore-vcs",
				"--no-ignore-parent", "--unrestricted", "--case-sensitive",
				"--ignore-case", "--glob", "--regex", "--fixed-strings",
				"--absolute-path", "--relative-path", "--follow", "--no-follow",
				"--full-path", "--print0", "--show-errors", "--one-file-system",
				"--prune", "--quiet", "--version", "--strip-cwd-prefix"),
			numOf("-d", "--max-depth", "--min-depth", "--exact-depth", "--max-results",
				"-j", "--threads"),
			strOf("-t", "--type", "-e", "--extension", "-E", "--exclude", "-S", "--size",
				"--changed-within", "--changed-before", "--older", "--newer",
				"-o", "--owner", "--base-directory", "--path-separator", "--format",
				"--color", "--ignore-file", "--and", "--search-path"),
		),
		doubleDash: true, maxPositionals: anyPositionals,
	},

	// --- Network-facing ------------------------------------------------------
	"gh": {
		// P67.8 opens up read-only gh subcommands — but as tool.CapNetwork,
		// not tool.CapRead. Every gh call talks to GitHub, and plan mode Asks
		// for CapNetwork while it Allows CapRead silently (permission.Policy.
		// Decide). Classifying gh as CapRead would make it *more* permissive
		// than the network gate it must sit behind. The downgrade is still
		// worth having: build mode Allows CapNetwork where it Asks for
		// CapExecute, and plan mode Asks instead of Denying outright.
		//
		// DENIED, and why:
		//   `gh api` is a general-purpose HTTP client — `-X POST`, `-f k=v`
		//     mutate anything the token can reach. No read-only subset of it
		//     is expressible in a flag table.
		//   `gh auth` (status included) is one --show-token away from printing
		//     the credential, the same class of leak as env/printenv.
		//   -w/--web launches a browser: a program launcher, like less/more.
		//   --hostname points gh at an arbitrary host, i.e. sends the GitHub
		//     token somewhere else. Nothing in the name says "credential
		//     exfiltration".
		//   Any `create`/`edit`/`close`/`merge`/`delete`/`comment` subcommand
		//     mutates; only the view/list/status/diff family is listed.
		capability: tool.CapNetwork,
		subcommands: map[string]*commandSpec{
			"pr":       ghSubcommands("view", "list", "diff", "status", "checks"),
			"issue":    ghSubcommands("view", "list", "status"),
			"repo":     ghSubcommands("view", "list"),
			"run":      ghSubcommands("view", "list"),
			"release":  ghSubcommands("view", "list"),
			"workflow": ghSubcommands("view", "list"),
			"search":   ghSubcommands("repos", "issues", "prs", "code", "commits"),
			"label":    ghSubcommands("list"),
			"gist":     ghSubcommands("view", "list"),
		},
		maxPositionals: 0,
	},

	// --- PowerShell ----------------------------------------------------------
	// The shell tool runs commands through PowerShell on Windows (see
	// shell.go's Description), so the read-only tier needs cmdlet entries too.
	// All of them set singleDashLong: PowerShell parameters are whole words
	// behind one dash, matched case-insensitively, with no short-flag
	// clustering and no POSIX "--". PowerShell also accepts unambiguous
	// parameter *abbreviations* ("-Rec" for "-Recurse"), which this table does
	// not enumerate — an abbreviation simply fails closed and costs the call
	// its downgrade, which is the safe direction.
	"get-childitem": psSpec(
		noneOf("-recurse", "-force", "-name", "-file", "-directory", "-hidden",
			"-readonly", "-system", "-followsymlink", "-usetransaction"),
		strOf("-path", "-literalpath", "-filter", "-include", "-exclude",
			"-attributes", "-erroraction", "-errorvariable"),
		numOf("-depth"),
	),
	"get-content": psSpec(
		noneOf("-raw", "-force", "-wait", "-asbytestream", "-usetransaction"),
		strOf("-path", "-literalpath", "-filter", "-include", "-exclude",
			"-encoding", "-delimiter", "-stream", "-erroraction"),
		numOf("-tail", "-totalcount", "-head", "-first", "-last", "-readcount"),
	),
	"get-item": psSpec(
		noneOf("-force", "-usetransaction"),
		strOf("-path", "-literalpath", "-filter", "-include", "-exclude", "-stream",
			"-erroraction"),
	),
	"test-path": psSpec(
		noneOf("-isvalid", "-newerthan", "-olderthan", "-usetransaction"),
		strOf("-path", "-literalpath", "-filter", "-include", "-exclude",
			"-pathtype", "-erroraction"),
	),
	"get-location": psSpec(
		noneOf("-stack"),
		strOf("-psprovider", "-psdrive", "-stackname"),
	),
	// Get-Process survives while `ps` does not: its default output is a
	// process table with no environment block, and PowerShell exposes a
	// process's environment only through .StartInfo.Environment, which needs a
	// property access this classifier's metacharacter rejection already
	// forbids. -ComputerName is denied because it reaches a *remote* machine.
	"get-process": psSpec(
		noneOf("-module", "-fileversioninfo", "-includeusername"),
		strOf("-name", "-inputobject", "-erroraction"),
		numOf("-id"),
	),
	// Unlike POSIX `date`, Get-Date has no clock-setting form at all — that is
	// Set-Date, a different cmdlet — so no predicate is needed here.
	"get-date": psSpec(
		noneOf("-asutc"),
		strOf("-format", "-uformat", "-date", "-displayhint"),
		numOf("-year", "-month", "-day", "-hour", "-minute", "-second", "-millisecond"),
	),
	"select-string": psSpec(
		noneOf("-simplematch", "-casesensitive", "-notmatch", "-list", "-quiet",
			"-allmatches", "-raw", "-noemphasis"),
		strOf("-path", "-literalpath", "-pattern", "-include", "-exclude",
			"-encoding", "-inputobject", "-erroraction"),
		numOf("-context"),
	),
	// where.exe (the Windows binary, not PowerShell's Where-Object) takes
	// slash-style switches this parser treats as positionals; a "/R dir"
	// invocation therefore fails path confinement and simply loses the
	// downgrade.
	"where": {flags: flagsOf(), maxPositionals: anyPositionals},
}

// ghSubcommands builds the leaf spec for a gh noun's read-only verbs. Every
// verb shares one flag table: gh's output-shaping flags (--json/--jq/
// --template), its filters, and nothing that mutates, opens a browser, or
// retargets the host. See the gh entry for what is denied and why.
func ghSubcommands(verbs ...string) *commandSpec {
	leaf := &commandSpec{
		capability: tool.CapNetwork,
		flags: flagsOf(
			noneOf("-c", "--comments", "--patch", "--name-only", "--merged", "--closed",
				"--json-fields", "--no-color"),
			numOf("-L", "--limit"),
			strOf("--json", "-q", "--jq", "-t", "--template", "-R", "--repo",
				"-s", "--state", "-A", "--author", "-a", "--assignee", "-l", "--label",
				"-S", "--search", "-B", "--branch", "--head", "--base", "--owner",
				"--sort", "--order", "--visibility", "--language", "--topic",
				"--workflow", "--user", "--filename"),
		),
		doubleDash: true, maxPositionals: 2,
	}
	m := make(map[string]*commandSpec, len(verbs))
	for _, v := range verbs {
		m[v] = leaf
	}
	return &commandSpec{capability: tool.CapNetwork, subcommands: m, maxPositionals: 0}
}

// psSpec builds a PowerShell cmdlet spec: single-dash long parameters, no
// POSIX "--", unlimited positionals.
func psSpec(groups ...flagGroup) *commandSpec {
	return &commandSpec{
		flags:          flagsOf(groups...),
		singleDashLong: true,
		maxPositionals: anyPositionals,
	}
}

// readOnlyGitSubcommands is the allowlist of git subcommands treated as
// read-only. Only subcommands that are read-only for every possible
// argument combination belong here — e.g. "branch"/"tag"/"remote" are
// excluded because a positional argument turns them into a mutation
// ("git branch foo" creates a branch), and "reflog"/"symbolic-ref" because
// their extra forms ("reflog expire", "symbolic-ref NAME REF") mutate too.
// The listed ones stay read-only regardless of extra flags or pathspecs.
//
// git keeps its own path (rather than a commandSpec) on purpose: the flag
// rules for a git invocation must stay *identical* to the dedicated git tool's,
// and validateReadOnlyGitArgv in argv_confine.go is the one place that decides
// them for both. Duplicating them into a flag table here would recreate
// exactly the divergence P66.3 closed.
var readOnlyGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true, "blame": true,
	"annotate": true, "rev-parse": true, "describe": true, "ls-files": true,
	"ls-tree": true, "cat-file": true, "shortlog": true, "grep": true,
	"rev-list": true, "diff-tree": true, "diff-index": true, "merge-base": true,
	"name-rev": true, "show-branch": true, "count-objects": true,
	"whatchanged": true, "for-each-ref": true, "check-ignore": true,
}

// shellChainMetaCharsNoPipe is permission.ShellChainMetaChars with "|"
// removed. classifyShellCommand splits a command on top-level pipes itself
// and classifies each stage independently (see below); every other
// chaining/redirection/substitution character — ";", "&", backticks, "$()",
// "<>", newlines — still refuses the downgrade outright, exactly as before.
const shellChainMetaCharsNoPipe = `;&` + "`" + `$()<>` + "\n\r"

// classifyShellCommand reports the capability a shell command may be gated as
// instead of tool.CapExecute (P25.4c, P67.8), and whether it is classified at
// all. The command is rejected outright if any shell chaining/redirection/
// substitution metacharacter other than "|" is present anywhere in the
// string — including one nested inside quotes, which this scan does not
// parse — if its binary has no entry in readOnlyShellCommands, if it carries
// a flag that entry does not list, or if any argument resolves (via
// sandbox.ValidatePath, the same root-confinement check read_file/grep/glob
// already use) outside root (P32.1).
//
// A top-level "|" is handled by splitting the command into pipeline stages —
// each classified independently by classifySingleShellCommand, applying the
// exact same allowlist, flag parsing and root confinement to every stage —
// rather than by adding a pipe-aware grammar to the per-command flag tables.
// The split is deliberately blunt: it cuts on every literal "|" byte with no
// quote-awareness at all, the same posture the metacharacter scan above
// already takes ("does not parse, fails closed"). A quoted pipe inside a
// single stage — grep -e 'a|b' — therefore produces a stage whose quote never
// closes; splitShellWords reports that as unparseable and the whole pipeline
// loses its downgrade rather than being silently misread. An empty stage
// (a leading/trailing "|", or "||") is refused for the same reason: it is not
// a construct this parser claims to understand.
//
// A pipeline classifies as CapNetwork if any stage does (gh is the only
// entry that does), CapRead otherwise; every stage must classify for the
// pipeline to. Being conservative here is deliberate: a false negative just
// means the call keeps requiring an execute approval like today; a false
// positive would auto-approve something that mutates state or exfiltrates
// data.
//
// powershell selects the quoting dialect the command will actually be run
// under, which decides only one thing: whether a backslash escapes the next
// character (POSIX `/bin/sh -c`) or is an ordinary path separator (PowerShell).
// Getting that backwards is a hole in either direction, so it is a parameter
// rather than a guess — see splitShellWords.
func classifyShellCommand(root, command string, powershell bool) (tool.Capability, bool) {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, shellChainMetaCharsNoPipe) {
		return tool.CapExecute, false
	}
	// P81.20/FIND-20: reject any byte the allowlist below does not name, before
	// doing anything else with the string. ShellChainMetaChars is a denylist —
	// CRIT-1, CRIT-2 and P79.1 were each one more spelling that denylist had
	// not been told about yet (a bare "~", an unconfined argv0, a Windows
	// drive-letter path), found only once each was exploited or fuzzed into
	// existence. commandCharsetAllowed inverts that: every character permitted
	// here is one a real read-only invocation in the table below needs —
	// checked against the whole existing test corpus — so a construct built
	// from anything else (a control byte, a shell operator nobody has named
	// yet, an encoding trick) fails closed on being unrecognized rather than
	// on being recognized as bad.
	if !commandCharsetAllowed(command) {
		return tool.CapExecute, false
	}
	stages := strings.Split(command, "|")
	overall := tool.CapRead
	for _, stage := range stages {
		stage = strings.TrimSpace(stage)
		if stage == "" {
			return tool.CapExecute, false
		}
		cap, ok := classifySingleShellCommand(root, stage, powershell)
		if !ok {
			return tool.CapExecute, false
		}
		if cap != tool.CapRead {
			overall = cap
		}
	}
	return overall, true
}

// classifySingleShellCommand is classifyShellCommand's per-stage worker: it
// classifies one already-trimmed, metacharacter-free, non-empty command —
// either the whole call, when there was no pipe, or one stage of a pipeline.
func classifySingleShellCommand(root, command string, powershell bool) (tool.Capability, bool) {
	// Split the way the shell will, not the way strings.Fields does. Quoting is
	// not decoration here: it changes what a token *is*, and the confinement
	// check below can only judge a path it can recognize as one. See
	// splitShellWords for the escape this closes.
	fields, ok := splitShellWords(command, !powershell)
	if !ok || len(fields) == 0 {
		return tool.CapExecute, false
	}
	// CRIT-2: baseBinaryName below reduces "./scripts/ls" to "ls", which hits
	// readOnlyShellCommands and classifies as CapRead — while argvStaysInRoot
	// is only ever handed fields[1:], so token zero is never validated as a
	// path at all. `shell({"command":"./scripts/ls"})` against a cloned repo
	// (git preserves the executable bit, so this needs no write and works in
	// plan mode) therefore executed attacker-chosen code with no approval, no
	// checkpoint (captureShellWrites is keyed on the same capability), and no
	// exec lock. Refuse the downgrade for a path-qualified argv0 — the read-only
	// tier is for a bare command name resolved through PATH, which is a binary
	// the *operator* installed. Do not "fix" this by confining argv0 to the
	// workspace instead: a workspace-resident executable is precisely the
	// attack, so rejection rather than confinement is the correct posture.
	if pathQualifiedBinary(fields[0]) {
		return tool.CapExecute, false
	}
	bin := strings.ToLower(baseBinaryName(fields[0]))
	if bin == "git" {
		if readOnlyGitCommand(root, fields[1:]) {
			return tool.CapRead, true
		}
		return tool.CapExecute, false
	}
	spec := readOnlyShellCommands[bin]
	if spec == nil {
		return tool.CapExecute, false
	}
	cap, classified := spec.classify(fields[1:])
	if !classified {
		return tool.CapExecute, false
	}
	// argvStaysInRoot (argv_confine.go) confines operands *and* attached flag
	// values; the old local helper skipped every token starting with "-",
	// which is what VULN-02 rode in on. Flag parsing above decided whether the
	// command *can* be read-only; this decides whether this invocation is —
	// both, never either. A path outside root disqualifies the command from
	// the downgrade — it still runs, just under the normal CapExecute approval
	// flow instead of being silently auto-allowed under plan mode's read gate.
	if !argvStaysInRoot(root, spec.confinementArgs(fields[1:])) {
		return tool.CapExecute, false
	}
	return cap, true
}

// shellCommandPaths resolves the filesystem paths a shell command's argv
// names, for the engine's parallel-round dependency graph (P81.30 / FIND-30).
// It walks the argv the same way classifyShellCommand does — splitShellWords,
// baseBinaryName/pathQualifiedBinary, the git-subcommand split, and
// confinementArgs/argvPathCandidates — rather than re-parsing the command a
// second way. Unlike classifyShellCommand it does not call
// sandbox.ValidatePath: this function answers "what does this command's argv
// name", not "is that name confined to root", and the engine only needs the
// former to order same-path calls against each other. It also does not
// require the binary to be one of the read-only commands — a write/execute
// command's paths matter to the graph exactly as much as a read's, since a
// later call on the same path must still wait for it.
//
// resolved is false whenever the argv cannot be attributed to path-bearing
// operands with confidence: a command carrying a shell chaining/redirection/
// substitution metacharacter (which can name a target this parse never sees,
// e.g. "> /tmp/x"), one splitShellWords cannot tokenize, a path-qualified
// argv0 (CRIT-2's concern — a workspace-resident script, not a fixed
// operator-installed binary), or one naming a candidate the shell would
// expand before this parse ever sees the literal target (a tilde or a glob).
// The caller treats an unresolved command as touching anything a concurrent
// write in the round might, the same fail-closed direction
// classifyShellCommand already takes for capability.
//
// A command that parses cleanly but names no path-shaped operand at all (a
// bare `pwd`, `git status`) is resolved=true with an empty path list, which
// participates in no ordering — the same as a tool call with no "path" field.
func shellCommandPaths(command string, powershell bool) (paths []string, resolved bool) {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, permission.ShellChainMetaChars) {
		return nil, false
	}
	fields, ok := splitShellWords(command, !powershell)
	if !ok || len(fields) == 0 || pathQualifiedBinary(fields[0]) {
		return nil, false
	}
	bin := strings.ToLower(baseBinaryName(fields[0]))
	args := fields[1:]
	switch {
	case bin == "git":
		if len(fields) < 2 {
			return nil, true // "git" alone names nothing
		}
		args = fields[2:] // skip the subcommand, as validateReadOnlyGitArgv does
	case readOnlyShellCommands[bin] != nil:
		// PowerShell's colon-attached value ("-Path:C:\Windows") — the same
		// adaptation confinementArgs performs before argvPathCandidates sees
		// the token, so a target spelled that way isn't missed here either.
		args = readOnlyShellCommands[bin].confinementArgs(args)
	}
	for _, a := range args {
		for _, c := range argvPathCandidates(a) {
			if c == "" {
				continue
			}
			if expandsToHome(c) || strings.ContainsAny(c, "*?[") {
				// The shell resolves this to a literal target this parse never
				// sees; matching it against another call's literal path would
				// silently miss the dependency, so the whole command is
				// unresolved instead.
				return nil, false
			}
			paths = append(paths, filepath.Clean(c))
		}
	}
	return paths, true
}

// commandCharsetAllowlist is every byte a bare word in the read-only command
// table can legitimately need: letters, digits, whitespace that
// splitShellWords already treats as a separator, and the punctuation the
// table's own flags/paths/patterns use — path separators, flag dashes,
// attached-value separators ("=", ":"), size/date-format punctuation ("+",
// "%", "."), glob metacharacters rg/fd/grep's own --glob/--include flags take
// as an ordinary string value ("*", "?", "[", "]"), and quote characters
// splitShellWords already parses structurally. "|" is here for the same
// reason: classifyShellCommand strips it out and splits on it itself before
// this check ever runs on a stage, so by the time a stage reaches this
// allowlist no "|" remains in it — but the allowlist is checked against the
// *original, unsplit* string first, so pipe-carrying pipelines need it listed
// here too. Bytes >= 0x80 (UTF-8 continuation/lead bytes) are allowed
// separately, not through this table — see commandCharsetAllowed — because a
// legitimate filename may be non-ASCII and this classifier does not want to
// be the reason it cannot be read.
//
// Deliberately absent: "!", "#", "^", "{", "}", and every ASCII control byte
// other than space/tab. None of them appears in any read-only command's flag
// or pattern spelling (checked against the whole test corpus when this table
// was written), and each has a history of being a shell metacharacter in some
// dialect this classifier does not want to have to keep discovering one
// exploit at a time. "(" and ")" are not repeated here — they are already
// refused by shellChainMetaCharsNoPipe above, checked first — but excluding
// them from this list too costs nothing and keeps this table readable as
// "yes" rather than "not one of the other list's noes".
const commandCharsetAllowlist = "" +
	"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" +
	" \t" +
	`-_./\~:=+,*?%'"@[]|`

// commandCharsetAllowed reports whether every byte of command is either in
// commandCharsetAllowlist or is part of a valid non-ASCII UTF-8 sequence (a
// legitimate filename in a language other than English). Invalid UTF-8 fails
// closed rather than being let through byte-by-byte — a malformed sequence is
// exactly the kind of "construct nobody described" this check exists to
// refuse.
func commandCharsetAllowed(command string) bool {
	if !utf8.ValidString(command) {
		return false
	}
	for _, r := range command {
		if r >= utf8.RuneSelf {
			continue // non-ASCII: valid UTF-8 already confirmed above
		}
		if !strings.ContainsRune(commandCharsetAllowlist, r) {
			return false
		}
	}
	return true
}

// splitShellWords splits a command into the argument vector the shell will
// actually build, removing quotes and (when escapeBackslash) backslash escapes.
//
// It replaces strings.Fields, which was the whole of VULN-14: Fields splits on
// whitespace and nothing else, so a quoted path reached the confinement check
// with its quotes still attached. `cat '/etc/passwd'` handed argvStaysInRoot the
// literal token `'/etc/passwd'`, which does not start with a separator, so
// sandbox.ValidatePath read it as a *relative* name, joined it under the root,
// found it confined, and the call was downgraded to CapRead — which plan mode
// allows silently. The shell then stripped the quotes and read the real file.
// Every spelling of that worked: `"..."`, `\/etc/passwd`, `”/etc/passwd`. Worst
// of all, deniedGitFlags compares raw tokens, so `git diff '--output=/tmp/x'`
// was not recognized as --output at all — an out-of-workspace *write*, silently
// approved. Quoting is not cosmetic to this classifier; it decides what a token
// is, and the classifier has to see what the shell will see.
//
// escapeBackslash must match the shell that will run the command, and the two
// dialects disagree in a way that is a hole in both directions. Under POSIX
// `/bin/sh -c` a backslash escapes the next character, so treating it literally
// lets `cat \/etc/passwd` through. Under PowerShell it is an ordinary path
// separator, so treating it as an escape turns `\Windows\System32\config\SAM`
// into the relative name `WindowsSystem32configSAM`, which confines happily —
// the same hole, mirrored. Hence a parameter rather than a default.
//
// It reports false for input the shell itself would reject or continue: an
// unterminated quote, or a trailing escape. Failing closed there costs the call
// its downgrade, which is the cheap direction.
//
// This is a *quoting* splitter, not a shell parser. It does not need to be one:
// classifyShellCommand has already refused the command outright if it contains
// any of ShellChainMetaChars, so expansion, substitution, redirection and
// chaining are all gone before a single token is built here.
func splitShellWords(command string, escapeBackslash bool) ([]string, bool) {
	var (
		words   []string
		cur     strings.Builder
		started bool // distinguishes an empty quoted word ("") from no word at all
		quote   byte // 0 when outside quotes, else '\'' or '"'
	)
	flush := func() {
		if started {
			words = append(words, cur.String())
			cur.Reset()
			started = false
		}
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote == '\'':
			// Single quotes are literal in both dialects: no escape processing,
			// which is why `'\/etc'` keeps its backslash.
			if c == '\'' {
				quote = 0
				continue
			}
			cur.WriteByte(c)
		case quote == '"':
			if c == '"' {
				quote = 0
				continue
			}
			if escapeBackslash && c == '\\' && i+1 < len(command) {
				i++
				cur.WriteByte(command[i])
				continue
			}
			cur.WriteByte(c)
		case c == ' ' || c == '\t':
			flush()
		case c == '\'' || c == '"':
			quote = c
			started = true
		case escapeBackslash && c == '\\':
			if i+1 >= len(command) {
				return nil, false // a trailing escape continues the line
			}
			i++
			cur.WriteByte(command[i])
			started = true
		default:
			// Byte-wise is safe for UTF-8: none of the bytes handled above can
			// appear as a continuation byte, so multi-byte runes pass intact.
			cur.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, false // unterminated quote
	}
	flush()
	return words, true
}

// confinementArgs adapts an argv for argvPathCandidates, which knows the two
// POSIX attached-value spellings ("--flag=value", "-fvalue") but not
// PowerShell's third one: "-Path:C:\Windows" attaches its value with a colon.
// Left alone, argvPathCandidates reads that token as the short-cluster form
// and offers "ath:C:\Windows" — a relative name that resolves happily inside
// the root, so the escape passes. Splitting the value out here hands it over
// as an operand instead. This is the VULN-02 shape in a third spelling; a
// missed attached value is a host-filesystem read that plan mode allows in
// silence.
func (s *commandSpec) confinementArgs(args []string) []string {
	if !s.singleDashLong {
		return args
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			if name, value, ok := strings.Cut(a, ":"); ok {
				out = append(out, name, value)
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// readOnlyShellCommand reports whether command is safe to gate as
// tool.CapRead. It is the narrower question classifyShellCommand answers:
// a command classified as tool.CapNetwork (gh) is not a *read*, so it neither
// gets plan mode's silent allow nor counts as read-only anywhere else.
//
// The quoting dialect is the host's own, which is what production resolves to:
// a Windows host reaches classifyShellCommand through shellTool.usesPowerShell()
// and every other host runs `/bin/sh -c`. It used to hardcode POSIX, and that
// was EXEC-4 — on Windows the POSIX backslash-escape rule collapsed
// `C:\Users\x\.ssh\id_rsa` into the relative token `C:Usersx.sshid_rsa`, which
// confined happily inside the root. Four tests failed on Windows because of it,
// two of them skipped on Linux as well, so those two had never passed on any
// platform while pinning the P32.1 drive-letter escape. Use
// readOnlyShellCommandIn where a test means one specific dialect regardless of
// where it runs.
func readOnlyShellCommand(root, command string) bool {
	return readOnlyShellCommandIn(root, command, runtime.GOOS == "windows")
}

// readOnlyShellCommandIn is readOnlyShellCommand with the quoting dialect named
// explicitly, for the cases that are about one dialect rather than about the
// host: getting the dialect backwards is a hole in either direction, so it is
// never guessed. See splitShellWords.
func readOnlyShellCommandIn(root, command string, powershell bool) bool {
	cap, ok := classifyShellCommand(root, command, powershell)
	return ok && cap == tool.CapRead
}

// classify walks a command's arguments against its spec and returns the
// capability the invocation earns. Every unrecognized flag fails closed.
func (s *commandSpec) classify(args []string) (tool.Capability, bool) {
	cap := s.capability
	if cap == "" {
		cap = tool.CapRead
	}
	if s.subcommands != nil {
		if len(args) == 0 {
			return tool.CapExecute, false
		}
		sub := s.subcommands[strings.ToLower(args[0])]
		if sub == nil {
			return tool.CapExecute, false
		}
		return sub.classify(args[1:])
	}
	positionals, ok := s.parseFlags(args)
	if !ok {
		return tool.CapExecute, false
	}
	if s.maxPositionals != anyPositionals && len(positionals) > s.maxPositionals {
		return tool.CapExecute, false
	}
	if s.predicate != nil && !s.predicate(positionals) {
		return tool.CapExecute, false
	}
	return cap, true
}

// parseFlags splits args into positionals, rejecting any flag the spec does
// not list. A flag's value may be attached ("--glob=x", "-gx", "-Path:x") or
// separated ("--glob x"); a separated value is consumed here so it is never
// miscounted as a positional.
func (s *commandSpec) parseFlags(args []string) ([]string, bool) {
	var positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			// The per-command POSIX "--" switch. A command that does not
			// honor it sees "--" as an unlisted flag and fails closed, rather
			// than silently treating the rest of the line as operands the way
			// a "--"-honoring command would.
			if !s.doubleDash {
				return nil, false
			}
			positionals = append(positionals, args[i+1:]...)
			return positionals, true
		case a == "" || a == "-" || !strings.HasPrefix(a, "-"):
			positionals = append(positionals, a)
			continue
		}

		var name, value string
		var hasValue, cluster bool
		switch {
		case s.singleDashLong:
			// PowerShell: "-Recurse", "-Path x", "-Path:x". Case-insensitive.
			name, value, hasValue = strings.Cut(a, ":")
			name = strings.ToLower(name)
		case strings.HasPrefix(a, "--"):
			name, value, hasValue = strings.Cut(a, "=")
		default:
			if s.numericShorthand && isAllDigits(a[1:]) {
				continue // head/tail/uniq's obsolete "-20" count
			}
			cluster = true
		}

		if cluster {
			consumed, ok := s.parseShortCluster(a, args, i)
			if !ok {
				return nil, false
			}
			i = consumed
			continue
		}

		spec, listed := s.flags[name]
		if !listed {
			return nil, false
		}
		switch {
		case spec.arg == noArg:
			if hasValue {
				return nil, false // "--recursive=x" is not a flag we listed
			}
		case hasValue:
			if !spec.valid(value) {
				return nil, false
			}
		case spec.optional:
			// "--color" with no attached value: takes none. Deliberately does
			// not reach forward for the next token, which would swallow a path
			// operand.
		default:
			if i+1 >= len(args) || !spec.valid(args[i+1]) {
				return nil, false
			}
			i++
		}
	}
	return positionals, true
}

// parseShortCluster handles a single-dash token as a cluster of short flags
// ("-la", "-n20", "-ofoo"). It returns the index of the last args element it
// consumed. The first flag in the cluster that takes a value takes the rest of
// the token as that value, or the next argument when the token ends there.
func (s *commandSpec) parseShortCluster(a string, args []string, i int) (int, bool) {
	for j := 1; j < len(a); j++ {
		name := "-" + string(a[j])
		spec, listed := s.flags[name]
		if !listed {
			return 0, false
		}
		if spec.arg == noArg {
			continue
		}
		if rest := a[j+1:]; rest != "" {
			if !spec.valid(rest) {
				return 0, false
			}
			return i, true
		}
		if spec.optional {
			return i, true
		}
		if i+1 >= len(args) || !spec.valid(args[i+1]) {
			return 0, false
		}
		return i + 1, true
	}
	return i, true
}

// valid reports whether value satisfies the flag's argument type and pattern.
func (f flagSpec) valid(value string) bool {
	if f.arg == numArg {
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return false
		}
	}
	if f.pattern != nil && !f.pattern.MatchString(value) {
		return false
	}
	return true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// readOnlyGitCommand reports whether a git invocation's arguments (fields
// after "git") are a read-only status/log/diff call. Past the subcommand
// allowlist it defers entirely to validateReadOnlyGitArgv, which is the same
// check the dedicated git tool runs on the same argv — the two used to carry
// divergent denylists and only one of them confined paths (P66.3).
func readOnlyGitCommand(root string, args []string) bool {
	if len(args) == 0 || !readOnlyGitSubcommands[strings.ToLower(args[0])] {
		return false
	}
	return validateReadOnlyGitArgv(root, args) == nil
}

// pathQualifiedBinary reports whether argv0 names a *file* rather than a bare
// command the host would resolve through PATH: it carries a path separator, is
// rooted (sandbox.IsRooted, the single authority on "the OS resolves this from
// a filesystem root" — including the Windows spellings filepath.IsAbs answers
// false for), or begins with a tilde the shell will expand to a home directory.
// See the CRIT-2 note in classifyShellCommand for why such a binary is refused
// the read-only downgrade outright.
func pathQualifiedBinary(argv0 string) bool {
	if argv0 == "" {
		return false
	}
	if strings.ContainsAny(argv0, `/\`) || strings.HasPrefix(argv0, "~") {
		return true
	}
	return sandbox.IsRooted(argv0)
}

// baseBinaryName strips a path prefix and, on Windows, a .exe/.cmd/.bat
// suffix, so "/bin/cat" and "cat.exe" both classify the same as "cat".
func baseBinaryName(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	lower := strings.ToLower(s)
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		if strings.HasSuffix(lower, ext) {
			return s[:len(s)-len(ext)]
		}
	}
	return s
}
