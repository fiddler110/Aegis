---
description: Generate a PowerPoint presentation using AutoDeckBuilder. Use when the user asks to build, create, or generate a deck, slides, or presentation. Understands the full YAML schema and runs deckbuilder.py.
---

# AutoDeckBuilder Skill

You are operating inside the Aegis project. Your job is to:
1. Understand what the user wants to present
2. Write a valid YAML file following the schema below
3. Run `python deckbuilder.py <file>` to produce the `.pptx`
4. Report the output path

`deckbuilder.py` is included at the root of this project. Write YAML files and output `.pptx` files into a `decks/` subdirectory (create it if it doesn't exist).

---

## Workflow

1. **Clarify** — ask one question if the topic, audience, or key content is unclear. Do not ask for more than what you need.
2. **Plan the slide sequence** — think about narrative flow: title → dividers → content → closing quote.
3. **Write the YAML** — follow the schema exactly. Default theme is `canadian_tire` unless the user specifies otherwise.
4. **Run the build** — `python deckbuilder.py <yaml_path>` from the repo root.
5. **Report** — tell the user the output path. If the build fails, show the error and fix the YAML.

---

## YAML File Structure

```yaml
meta:
  output: "decks/my_deck.pptx"       # output file path
  footer: "Company | Dept | 2025"    # right-aligned footer on every content slide
  theme: canadian_tire                # see theme list below
  logo: "assets/CantireLogo.png"     # optional; canadian_tire uses this automatically

slides:
  - type: <slide_type>
    ...
```

---

## Slide Types Reference

### `title` — Cover slide (use first)
```yaml
- type: title
  title: "Presentation Title"
  subtitle: "Optional subtitle"      # optional
  context: "Author · Date"           # optional small line
```

### `divider` — Section break
```yaml
- type: divider
  title: "Section Name"
  subtitle: "Optional tagline"       # optional
  section_number: 1                  # optional — renders "SECTION 1" label
  bg_color: navy                     # optional palette key or hex
```

### `bullets` — Bullet list
```yaml
- type: bullets
  title: "Slide Title"
  subtitle: "Optional subtitle"
  intro: "Optional bold intro sentence."
  bullets:
    - "Top-level bullet"
    - "  Sub-bullet (2 leading spaces = level 1)"
    - "    Deeper sub-bullet (4 spaces = level 2)"
```
Or explicit form:
```yaml
bullets:
  - text: "Item"
    level: 0    # 0, 1, or 2
```

### `cards` — Column cards (2–4 columns)
```yaml
- type: cards
  title: "Slide Title"
  subtitle: "Optional subtitle"
  intro: "Optional bold intro above cards."
  card_height: 4.8          # optional, inches
  columns:
    - title: "Column One"
      accent: primary       # palette key or hex
      fill: white           # card background
      title_size: 13        # optional font pt
      bullet_size: 9.5      # optional font pt
      bullets:
        - "Item one"
        - "Item two"
    - title: "Column Two"
      accent: blue
      bullets:
        - "Item three"
  takeaway: "Optional full-width summary card at the bottom."
```

### `card_grid` — Multi-row card grid
**Mode 1 — rows:**
```yaml
- type: card_grid
  title: "Grid Title"
  rows:
    - columns:
        - title: "Card A"
          accent: primary
          bullets: ["Detail one", "Detail two"]
        - title: "Card B"
          accent: blue
          bullets: ["Detail one"]
  badges: ["Label A", "Label B"]
  takeaway: "Optional summary."
```
**Mode 2 — two labelled sections:**
```yaml
- type: card_grid
  title: "Grid Title"
  sections:
    - label: "Before"
      color: primary
      rows:
        - columns:
            - title: "Card X"
              bullets: ["Point one"]
    - label: "After"
      color: teal
      rows:
        - columns:
            - title: "Card Y"
              bullets: ["Point one"]
```

### `decision_risk` — Decision boxes + RAG risk grid
```yaml
- type: decision_risk
  title: "Decisions & Risks"
  decisions:
    - title: "Decision Alpha"
      body: "Owner: Team A\nTimeline: Q3\nOption 1 vs Option 2"
      color: primary
    - title: "Decision Beta"
      body: "Owner: Team B\nTimeline: Q4"
      color: orange
  risks:
    high_color: primary
    high:
      - "High risk item"
    medium_color: orange
    medium:
      - "Medium risk item"
    low_color: green
    low:
      - "Low risk item"
```
Supports 1–6 decisions. Omit `decisions:` or `risks:` to render just one section.

### `decisions_heatmap` — Decisions panel + heatmap table
```yaml
- type: decisions_heatmap
  title: "Risk Heatmap"
  decisions:
    title: "Decisions required"
    color: primary
    items:
      - "Decision one"
  heatmap:
    title: "Risk Summary"
    headers: ["Domain", "Q1", "Q2", "Q3", "Q4"]
    rows:
      - label: "Area A"
        values: ["Low", "Medium", "High", "Low"]
  risk_themes:
    title: "Key themes"
    color: orange
    items:
      - "Theme one"
  takeaway:
    title: "Takeaway"
    text: "Summary sentence."
```
Heatmap values: `"High"` / `"Medium"` / `"Low"` (auto colour-coded).

### `dependency` — 3-column flow diagram
```yaml
- type: dependency
  title: "Flow Diagram"
  col1_label: "Input"
  col2_label: "Action"
  col3_label: "Outcome"
  nodes:
    - left: "Item Alpha\nOwner: Team A"
      middle: "Action description"
      right: "Desired outcome"
      color: teal
```
Up to ~6 rows; use `\n` for line breaks within cells.

### `timeline` — Phase roadmap (Now / Next / Later)
```yaml
- type: timeline
  title: "Roadmap"
  phases:
    - title: "Now"
      accent: primary
      period: "Q3 2026"
      bullets:
        - "Deliverable one"
    - title: "Next"
      accent: blue
      period: "Q4 2026"
      bullets:
        - "Deliverable two"
    - title: "Later"
      accent: teal
      bullets:
        - "Deliverable three"
```
2–4 phases work best.

### `metrics` — KPI / big-number boxes
```yaml
- type: metrics
  title: "KPIs"
  metrics:
    - value: "94%"
      label: "Metric Name"
      sub: "+10pp vs last period"   # optional
      color: primary
    - value: "142"
      label: "Second Metric"
      color: blue
  note: "Source note at bottom."   # optional
```
2–5 metrics; keep `value` under 6 characters.

### `quote` — Statement / closing quote
```yaml
- type: quote
  quote: "The quote text goes here — keep it impactful and under 3 lines."
  attribution: "Author Name, Context"   # optional
```

### `table` — Data table
```yaml
- type: table
  title: "Table Title"
  headers:
    - "Column A"
    - "Column B"
    - "Column C"
  rows:
    - ["Row 1 Col 1", "Row 1 Col 2", "Row 1 Col 3"]
    - ["Row 2 Col 1", "Row 2 Col 2", "Row 2 Col 3"]
```
Up to ~10 rows.

---

## Type Aliases

| Primary             | Also accepts                              |
|---------------------|-------------------------------------------|
| `bullets`           | `bullet`, `list`                          |
| `cards`             | `columns`, `three_column`, `two_column`   |
| `card_grid`         | `grid`, `multi_row`, `two_section`        |
| `decision_risk`     | `decisions`, `risk`, `rag`                |
| `decisions_heatmap` | `risk_heatmap`, `heatmap`                 |
| `dependency`        | `flow`, `pipeline`                        |
| `timeline`          | `roadmap`, `phases`, `now_next_later`     |
| `quote`             | `statement`, `callout`                    |
| `metrics`           | `kpi`, `numbers`                          |
| `table`             | `data`                                    |
| `divider`           | `section`, `chapter`                      |
| `title`             | `cover`                                   |

---

## Themes

| Name               | Primary Colour | Best For                              |
|--------------------|----------------|---------------------------------------|
| `canadian_tire`    | `#ED1C24`      | Default — CTC brand                   |
| `corporate_red`    | `#CE1126`      | Security, risk messaging              |
| `corporate_blue`   | `#1A5276`      | Technology, platform, infrastructure  |
| `corporate_green`  | `#1E8449`      | Sustainability, finance, delivery     |
| `modern_purple`    | `#8E44AD`      | Innovation, product, AI/data          |
| `minimal_slate`    | `#2C3E50`      | Clean, neutral                        |
| `executive_dark`   | `#E74C3C`      | High-contrast, dark accent            |

Custom theme override (partial):
```yaml
meta:
  theme:
    base: corporate_blue
    primary: "#FF6600"
```

---

## Colour Palette Keys

Use these names anywhere a colour is accepted (`accent:`, `color:`, `fill:`, `bg_color:`):

| Key          | Typical Meaning                                      |
|--------------|------------------------------------------------------|
| `primary`    | Main accent — top bars, badges, critical items       |
| `dark`       | Body text, card titles                               |
| `navy`       | Title/divider backgrounds, dark emphasis             |
| `blue`       | Secondary accent for cards, decision boxes           |
| `teal`       | Third accent                                         |
| `green`      | Positive / delivered / LOW risk                      |
| `orange`     | Warning / at-risk / MEDIUM risk                      |
| `light_gray` | Card backgrounds, table row shading                  |
| `mid_gray`   | Footer text, subtitles                               |
| `white`      | Text on colour, card fills                           |

Hex values are also accepted anywhere: `accent: "#E74C3C"`.

---

## CLI Commands

```bash
# Build a deck
python deckbuilder.py decks/my_deck.yaml

# Build with custom output path
python deckbuilder.py decks/my_deck.yaml -o decks/my_deck.pptx

# Build with a different theme
python deckbuilder.py decks/my_deck.yaml --theme corporate_blue

# List all themes
python deckbuilder.py --list-themes

# Generate a starter YAML template
python deckbuilder.py --init-yaml decks/starter.yaml
```

---

## Design Guidelines

- **Max ~7 bullets per slide** — split if more
- **Slide narrative**: title → divider → content slides → closing quote
- **Colour for meaning**: primary = critical/action, orange = caution, green = positive
- **Cards**: 3 columns is the sweet spot; 4 works with short text
- **Timeline**: 3 phases is ideal; keep bullets to one line each
- **Metrics**: 4 is the sweet spot; keep `value` short (≤ 6 chars)
- **Table**: ≤ 10 rows before it becomes hard to read

---

## Example: minimal working deck

```yaml
meta:
  output: "decks/my_deck.pptx"
  theme: corporate_blue

slides:
  - type: title
    title: "My Presentation"
    subtitle: "Example subtitle"

  - type: bullets
    title: "Key Points"
    bullets:
      - "First point"
      - "Second point"
      - "  Sub-point"

  - type: quote
    quote: "Closing statement."
    attribution: "Author, Date"
```
