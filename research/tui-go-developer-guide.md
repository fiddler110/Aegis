# Research Report: Developing an AI Agent TUI in Go — Efficient & Complete Approach

## Executive Summary

The **most efficient and complete** way to develop a CLI / TUI application in Go that functions similarly to **OpenCode** or **Claude Code** today is the **Charm Bracelet v2 ecosystem** (Bubble Tea v2 + Lip Gloss v2 + Bubbles v2), combined with **Fantasy** for AI agent abstractions. This stack *is* what powers both OpenCode's successor (**Crush**) and represents the current gold standard in production Go TUI development as of 2025–2026.

---

## 1. Market Context: Reference Applications

### 1.1 Claude Code (TypeScript/Node.js)
- **Architecture**: Full React component tree rendered to ANSI escape sequences via a custom fork of Ink + Yoga layout engine for flexbox in terminal
- **Scale**: ~389 files, ~1,623 component patterns, ~85K lines of UI code
- **Key patterns used**: Virtual DOM rendering, virtualized message list streaming diffs, permission dialogs, Vim-mode input, agentic loop visualization
- **Weakness for Go devs**: Entirely TypeScript/Node.js ecosystem; Ink doesn't have a direct Go equivalent

### 1.2 OpenCode → Crush (Go)
- Original OpenCode (2023–mid 2025): ~95K GitHub stars, MIT licensed, built on Bubble Tea v1
- **Archived mid-2025** by original author; project *continued under Charmbracelet as "Crush"* (~26K+ stars)
- Crush is the spiritual successor and *the* reference implementation today
- Crush architecture (from DeepWiki analysis):

```
┌─────────────────────────────────────────────────┐
│                    CLI Layer                     │
│              (Cobra: root/cmd/run/server)        │
├─────────────────────────────────────────────────┤
│               App Orchestrator                   │
│          (Services: sessions, messages, ...)     │
├──────────────┬──────────────────┬───────────────┤
│   Agent      │    Tool System   │    TUI Layer  │
│ Coordinator  │(Shell/LSP/MCP)   │(Bubble Tea v2)│
│              │                  │               │
│ Fantasy API  │                  │ Lip Gloss     │
│ Abstraction  │                  │ Bubbles       │
├──────────────┴──────────────────┴───────────────┤
│        Persistence (SQLite via modernc.org)      │
└─────────────────────────────────────────────────┘
```

---

## 2. The Go TUI Framework Landscape

### 2.1 Primary Contenders Reviewed

| Framework | Paradigm | Stars | Maturity | Render Performance | AI-Ready Features |
|-----------|----------|-------|----------|-------------------|------------------|
| **Bubble Tea v2** (Charm) | Elm Architecture / MVU | 43K+ GitHub | Production since 2019, v2 Feb 2026 | ★★★★★ Cursed Renderer | Built-in streaming, composability |
| **tview** | Immediate-mode widget tree | ~8.5K | Stable, mature | ★★★☆☆ | Good widgets, no native streaming |
| **Ratatui via Bindings** ⚠️ | Immediate mode (Rust) | N/A | Mature but Go bindings thin | ★★★★★ Rust-native | None in Go |
| **termui** | Event-driven | ~6K | Deprecated/arced | ★★☆☆☆ | Minimal widgets |
| **gocui** | Fullscreen editor paradigm | ~7K | Older, niche | ★★★☆☆ | Text-input focused only |

### 2.2 Why Bubble Tea v2 is the Winner

1. **Proven at Scale**: Powers Crush (the OpenCode successor), gh (GitHub CLI), lazygit, and 25,000+ open-source applications
2. **Cursed Renderer** (v2): A ground-up rewrite based on ncurses algorithm — "orders of magnitude" faster rendering, dramatically reduced per-frame cost
3. **MVU / Elm Architecture**: Clean separation of state → view → update aligns naturally with agentic loop patterns where UI must react to streaming LLM output and tool events
4. **Charm Ecosystem Completeness**: Lip Gloss (styling/layout), Bubbles (composable components like list, textinput), Glamour (Markdown rendering), Ultraviolet (syntax highlighting) — *everything* needed for a Claude Code-like experience
5. **V2 Features Critical for AI Agents**:
   - Higher-fidelity keyboard & mouse handling
   - Inline images in terminal
   - Clipboard transfer over SSH
   - Advanced compositing & layers
   - Declarative views (predictable output)
   - Native clipboard support
   - Color downsampling

---

## 3. Core Stack Recommendation

### 3.1 Complete Production-Ready Toolkit

```
┌─────────────────────────────────────────────────────────┐
│                    APPLICATION                           │
├──────────┬────────────┬─────────────┬───────────────────┤
│Bubble    │ Lip Gloss  │ Bubbles     │ Glamour           │
│ Tea v2   │ v2         │ v2          │ v2                │
│ TUI Core │ Styling    │ Widgets:    │ Markdown rendering│
│ (MVU)    │ & Layout   │ textinput,  │ (for code output) │
│          │            │ list, table,│                   │
│          │            │ spinner,... │                   │
├──────────┴────────────┴─────────────┴───────────────────┤
│                  AGENT / LAYER                           │
│  Fantasy (charm.land/fantasy) + Catwalk                  │
│  ─ Multi-provider AI SDK   ─ Model metadata registry    │
│  ─ Streaming agents        ─ Step event callbacks       │
├───────────────┬───────────────────┬─────────────────────┤
│ SPI13/Cobra   │ Modernc/SQLite    │ mvdan.cc/sh/v3     │
│ CLI Parsing   │ (CGO-free DB)     │ Shell emulation    │
└───────────────┴───────────────────┴─────────────────────┘
```

### 3.2 Key Dependencies

| Package | Purpose | Import Path |
|---------|---------|-------------|
| **Bubble Tea** | TUI framework (MVU loop) | `charm.land/bubbletea/v2` or `github.com/charmbracelet/bubbletea/v2` |
| **Lip Gloss** | Terminal styling, layout, borders, padding | `charm.land/lipgloss/v2` |
| **Bubbles** | Composable widgets (text input, list, spinner, listbox) | Various sub-imports under Bubbles module |
| **Fantasy** | Multi-provider AI agent SDK | `charm.land/fantasy` |
| **Catwalk** | Model metadata & capabilities | `charm.land/catwalk` |
| **Cobra** (or the Charm's newer command framework) | CLI parsing, subcommands, flags | `github.com/spf13/cobra` |
| **SQLC + modernc.org/sqlite** | Type-safe SQLite persistence (CGO-free) | `modernc.org/sqlite` |
| **mvdan.cc/sh/v3** | POSIX shell interpretation for agent tool calls | `mvdan.cc/sh/v3` |

---

## 4. Architecture Pattern: Mapping OpenCode/Crush Patterns to Go

### 4.1 Layered Service-Oriented Design (from Crush/Opencode)

```go
// All dependencies flow downward. No layer imports above it.

// ── CLI Entry Point (/cmd/root.go) ──────────────────────
// Parse flags → Load config → Init DB → Create App → Launch TUI

// ── Application Orchestrator (/internal/app/app.go) ─────
type App struct {
    Sessions    session.Service
    Messages    message.Service
    History     history.Service       // file change tracking
    Permissions permission.Service  // tool approval gates
    Agent       agent.Coordinator     // AI agent orchestrator
    LSP         *lsp.Manager          // language server connections
    Events      chan tea.Msg          // pubsub → TUI channel
}

// ── Agent Layer (/internal/agent/) ──────────────────────
type Coordinator struct { /* manages SessionAgent per session */ }
type SessionAgent struct { /* executes LLM tool loop via Fantasy */ }

// ── Tool System (/internal/tools/) ──────────────────────
var DefaultToolset = []fantasy.AgentTool{
    ShellTool(permissions, shell),
    FileWriteTool(permissions, fs),
    GlobTool(permissions, fs),
    GrepTool(permissions, fs),
    LSPTool(permissions, lspManager),
}

// ── TUI Layer (/internal/ui/) ───────────────────────────
func NewTUI(app *app.App) tea.Model {
    return &model{...}  // wraps sub-components: chat, dialogs
}

// ── Persistence (SQLite via SQLC) ───────────────────────
// Session CRUD → Message CRUD → History tracking
```

### 4.2 Event-Driven PubSub for Real-Time UI Updates

The critical pattern from OpenCode/Crush that enables smooth streaming:

```go
// 1. Backend publishes events from multiple services
subscribers := []func(pubsub.Event):{
    logger.Subscribe(ctx),        // log messages → status bar
    app.Sessions.Subscribe(ctx),  // session changes → sidebar
    app.Messages.Subscribe(ctx),  // new messages → chat view
    app.Permissions.Subscribe(),  // permission requests → dialog
    agent.Subscribe(),            // streaming output → render
}

// 2. All subscribers funnel into a single tea.Msg channel
for _, sub := range subscribers {
    go func(s sub) { 
        for evt := range s() {
            msgChannel <- transformToMsg(evt) 
        }
    }(sub)
}

// 3. TUI Update() consumes from this channel and calls msg() to continue MVU cycle
```

### 4.3 Chat Interface Components (Bubble Tea v2)

From Crush's UI architecture:

| Component | Bubble Tea Pattern | Purpose |
|-----------|-------------------|---------|
| **Main Layout** | `view.View` + Lip Gloss borders | Three-pane: sidebar, chat, status/footer |
| **Message List** | Custom render function over slice | Virtualized scrolling via only rendering visible chunks |
| **Streaming Agent Response** | View renders chunk-by-chunk from channel | Text appears progressively as LLM streams tokens |
| **Tool Call Display** | Collapsible Lip Gloss sub-views with syntax highlighting | Show tool input/output with code blocks |
| **File Diff Preview** | Split-pane view with Ultraviolet syntax highlight | Side-by-side diff rendering |
| **Permmission Dialog** | Modal overlay via `tea.WindowSizeMsg` handling | Confirmation prompts blocking main interaction |
| **Model/Command Picker** | `list.Model` from Bubbles module | Searchable, filterable dropdown lists |
| **Prompt Input** | `textinput.Model` + vim-mode binding | Multi-line input with history navigation |

---

## 5. Step-by-Step Implementation Blueprint

### Phase 1: Foundation (Core TUI Shell)

```go
// main.go — minimal working Bubble Tea v2 app
package main

import "charm.land/bubbletea/v2"

type model struct {
    quit   bool
    msg    string  
    width, height int
}

func (m model) Init() tea.Cmd                            { return nil }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.String() == "ctrl+c" || msg.String() == "q" { m.quit = true; return m, tea.Quit }
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
    }
    return m, nil
}
func (m model) View() string {
    if m.quit { return "bye\n" }
    return lipgloss.NewStyle().Width(m.width).Render("Hello! Type messages below.\n\n") + m.msg 
}

func main() {
    p := tea.New(model{})
    p.Run()
}
```

### Phase 2: Message/Chat System with Streaming

```go
type message struct {
    Role    string   // "user" | "assistant"
    Content string   // rendered text or markdown
    Tools   []toolCallResult
    Time    time.Time
}

type chatModel struct {
    messages  []message       // append-only list of full conversation
    input     textinput.Model
    rendering string          // live-updated by streaming updates  
    height    int
}
```

### Phase 3: Agent Integration with Fantasy

```go
// Provider setup (multi-provider from single code path)
provider, _ := openai.NewProvider(ctx, os.Getenv("OPENAI_API_KEY"))
model := provider.LanguageModel(ctx, "gpt-4o")       // or anthropic, google, etc.

tools = []fantasy.AgentTool{
    fantasy.NewAgentTool("shell", func(args shellArgs) {}),
    fantasy.NewAgentTool("read_file", func(p readPathArgs) (string, error) {
        return os.ReadFile(p.Path)
    }),
}

agent := fantasy.NewAgent(
    model,
    fantasy.WithTools(tools...),
    fantasy.WithSystemPrompt(yourPrompt),
)

// Streaming execution with real-time callbacks
err := agent.Stream(ctx, call, 
    func(step *fantasy.CallStep) error {
        // This fires on every LLM step:
        // - New assistant text → append to TUI message
        // - Tool calls queued → show progress indicator  
        // - Tool results received → render in chat
        messages <- formatAgentEvent(step)  // ← pubsub channel
        return nil
    })
```

### Phase 4: Dialog System, Commands & Sidebar

```go
// Crush's dialog approach — overlay rendering with state-based logic
type page int
const (
    pageChat page = iota
    pageCommandPalette
    pageModelSelect
    pageSessionSwitch
)

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch m.currentPage {
    case pageChat:
        // Route to chat sub-model or show modal overlay
        if m.modalDialog != nil {
            return m.renderOverlay(dialog), handleModalKeys(...)
        }
        return m.chat.Update(msg)
    case pageCommandPalette:
        return m.commandList.Update(msg)  // list.Model from bubbles
    }
}

func (m *model) View() string {
    sidebar := m.renderSidebar()       // always visible left panel
    content := m.renderContent()       // chat / command palette / settings
    footer := m.renderFooter()         // model name, keybindings hints
    
    return lipgloss.JoinVertical(lipgloss.Left,
        lipgloss.NewStyle().Width(m.width).Height(m.height-2).Render(
            lipgloss.JoinHorizontal(lipgloss.Top, sidebar, content)),
        footer,
    )
}

// Command palette registered via:
m.registerCommand("switch:model", "Switch AI model", m.doModelChange)
m.registerCommand("new:session",  "New conversation", m.doNewSession)
```

---

## 6. Essential Charm Ecosystem Modules Beyond the Core

| Module | Role | Why It Matters for an AI Coding Agent |
|--------|------|--------------------------------------|
| **Glamour** v2 | Markdown → TUI rendering | Render LLM responses (which include markdown) natively in terminal with proper formatting, code blocks, headers, lists, tables |
| **Ultraviolet** | Syntax-highlighted output streaming | Show shell command output and diff hunks with language-specific highlighting as they stream |
| **Huh!** | Form/input wizard framework | Build configuration wizards for first-run setup (API key entry, model selection) — though not needed inside the running app's main UI |
| **Gum** | CLI pipeable primitives (already built-in to Bubble Tea) | Command-line argument parsing and interactive filtering for non-TUI modes |
| **Bubbles / listbox** | Accessible multi-select lists | Session switching, provider/model picking with keyboard navigation |

---

## 7. Performance Considerations from Crush's Lessons

1. **Use the Cursed Renderer (Bubble Tea v2)** — The old Bubble Tea v1 renderer redrew the *entire terminal frame* on every Msg. v2's cell-based diffing only emits sequences for cells that changed, enabling smooth streaming of long LLM responses at high update frequency without flicker

2. **Keep View Functions Pure and Fast** — MVU means `View()` is called every tick. Heavy work (file I/O, API calls) belongs in async contexts; View must just format data structures

3. **Buffer Streaming Output** — Don't append to message content per-char for very long responses; batch into chunks from the Fantasy streaming callback

4. **Virtualize Long Message Lists** — Only render visible portion of conversation history (Crush's approach). Keep full list in a slice, but only format N rows that fit within viewport height

5. **Use `tea.Every` with Low Tick Rate for Idle Animations** — Spinners and status indicators tick at ~20-30 Hz, not 60fps; unnecessary high-frequency updates waste CPU and terminal bandwidth

---

## 8. Persistence & Database Strategy

Both OpenCode and Crush use:
- **modernc.org/sqlite** (CGO-free, single binary) — no external DB process required
- **SQLC** for type-safe query generation from `.sql` files
- Schema covering: sessions, messages, tool calls/metadata, permissions, file history

```sql
-- Example SQLC schema (sessions table)
CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL DEFAULT 'Untitled',
    model_id     TEXT NOT NULL,
    provider     TEXT NOT NULL,
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
    content     TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

---

## 9. Feature Parity Checklist (vs Claude Code / OpenCode)

| Feature | How to Implement in Go Charm Stack |
|---------|-------------------------------------|
| **Interactive chat** | Bubble Tea v2 main model + message list component |
| **Streaming responses** | Fantasy `agent.Stream()` + channel → TUI update loop |
| **Markdown rendering** | Glamour v2 to format assistant messages for View() |
| **Syntax highlighting** | Ultraviolet for code blocks in rendered output |
| **Multi-line prompt input** | Bubbles `textinput.Model` (supports Enter for newline) |
| **Vim-like mode** | Bind k/j/h/l keys → move cursor within textinput; implement modal logic explicitly (Bubble Tea doesn't include this out of the box but pattern is simple to add) |
| **Agentic tool loop** | Fantasy `Agent` + tool implementations via Bubble Tea v2 |
| **Permission dialogs** | Modal overlay in Bubble Tea View function |
| **Session management** | SQLite persistence with CRUD service layer |
| **Command palette** | Bubbles `list.Model` triggered by keybinding |
| **Model switching mid-session** | Fantasy model swap (`fantasy.WithLanguageModel(newModel)`) + UI re-render |
| **Shell tool execution** | `mvdan.cc/sh/v3` for POSIX shell parsing; render output with Ultraviolet |
| **File read/write tools** | Standard Go os.ReadFile/WriteFile wrapped as Fantasy AgentTools |
| **LSP integration** | LSP clients (e.g. via gopls or general-purpose go-lsp) wired into tool system for code intelligence |
| **MCP protocol support** | Implement MCP clients in Go to expose external tools/resources; wrap as Fantasy AgentTools |
| **Multi-provider models** | Fantasy provider abstraction (OpenAI, Anthropic, Google, Azure, Bedrock, OpenRouter via single API) |
| **Context compaction / summarization** | Custom logic: monitor token count (via Catwalk metadata), call agent.Summarize when threshold reached |

---

## 10. Build & Distribution

```yaml
# .goreleaser.yml — single binary, cross-platform
dist_builds:
  - goos: [linux, darwin, windows]
    goarch: [amd64, arm64]        # standard desktop/server targets
    env: [CGO_ENABLED=0]           # fully static binary (modernc/sqlite is CGO-free)
```

---

## 11. Summary & Recommendation Matrix

### Optimal Stack for a Go AI Agent TUI (Claude Code-like)

| Decision | Recommendation | Rationale |
|----------|---------------|-----------|
| **TUI Framework** | Bubble Tea v2 | Fastest Go option; powers Crush; Cursed Renderer; production-tested 6+ years |
| **Styling/Layout** | Lip Gloss v2 | Matches Charm ecosystem; declarative/composable; built on same renderer |
| **Widgets** | Bubbles v2 | textinput, list, spinner, table out of box |
| **AI Agent SDK** | Fantasy (charm.land/fantasy) | Multi-provider abstraction used by Crush; streaming tools; type safety |
| **CLI Parsing** | Cobra + Charm's own subcommands | Mature and well-documented |
| **Database** | SQLite via modernc.org/sqlite | CGO-free static binary |
| **Query Layer** | SQLC | Type-safe, compiled queries from `.sql` files |
| **Shell Tooling** | mvdan.cc/sh/v3 | POSIX shell interpreter in pure Go |
| **Markdown Rendering** | Glamour v2 | Required as LLMs output markdown extensively |
| **Syntax Highlighting** | Ultraviolet | Streaming code output with proper colors |

### Why NOT the Alternatives?

- **tview**: Good widgets but immediately-mode architecture doesn't fit the streaming/evented nature of AI agent apps; no compositing; steeper maintenance burden for complex layouts requiring layered/overlaid components
- **Inline Bubble Tea v1** (deprecated): Outperformed by v2's Cursed Renderer in streaming scenarios by "orders of magnitude"; all new features only land on v2 branch
- **Custom curses/termbox**: Unnecessary boilerplate; Charm ecosystem already solves ~95% of what you need

---

## 12. Quick Start Template Files

The complete minimal project structure would look like:

```
my-agent-tui/
├── cmd/
│   └── root.go           # Cobra CLI setup, entry point
├── internal/
│   ├── app/app.go        # App orchestrator (sessions, agent, services)
│   ├── agent/            # Fantasy Agent wiring + tool definitions
│   ├── db/db.go          # SQLite connection + SQLC queries
│   └── ui/               # Bubble Tea v2 models
│       ├── model.go      # Root TUI model (submodel routing)
│       ├── chat.go       # Chat view with message list + prompt input
│       └── dialogs.go    # Modal overlays (permissions, command palette)
├── config/
│   └── config.go         # Configuration loading/XDG dir handling
├── go.mod                # Go module declaration
├── main.go               # Import cmd package and Execute()
└── .goreleaser.yml       # Build/deploy configuration
```

---

*Research compiled: June 2026. Sources include Charm's official v2 announcement, Crush DeepWiki documentation, OpenCode pre-archival source code overview on GitHub, Charm Bracelet ecosystem repositories, and independent technical analyses from 2025–2026.*
