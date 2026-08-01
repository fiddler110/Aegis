#!/usr/bin/env bash
# Single-shot roadmap status report: repo state (uncommitted work, recent
# commits) + a structured parse of research/roadmap.md's open items, in
# document order (the roadmap already lists items in priority order within
# each track). Replaces the ad-hoc sequence of `git status` / `git diff
# --stat` / `git log` / manual grep-and-read of roadmap.md.
#
# Usage: scripts/roadmap-status.sh
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

ROADMAP="research/roadmap.md"

echo "=== Repo state ==="
git status --short
uncommitted=$(git status --porcelain | wc -l | tr -d ' ')
if [ "$uncommitted" -gt 0 ]; then
  echo
  echo "-- diffstat (uncommitted) --"
  git diff --stat
  git diff --cached --stat
fi
echo
echo "-- last 5 commits --"
git log --oneline -5
echo

echo "=== Roadmap Status summary (${ROADMAP}) ==="
awk '/^## Status/{flag=1; next} /^---/{flag=0} flag' "$ROADMAP" | sed '/^$/d'
echo

echo "=== Open items (document order = priority order within each track) ==="
awk '
  # First pass over the whole file: any line mentioning a "no concrete
  # trigger / not blocking / speculative" caveat gets every P<n>.<n> token
  # on that line recorded as globally blocked, even when the caveat sits in
  # a shared closing paragraph that covers more than one item (e.g. "Neither
  # P6.1 nor P6.5 is blocking...").
  FNR == NR {
    if ($0 ~ /[Nn]o concrete trigger|[Nn]ot [Bb]locking|speculative/) {
      line = $0
      while (match(line, /P[0-9]+(\.[0-9]+)?/)) {
        globalBlocked[substr(line, RSTART, RLENGTH)] = 1
        line = substr(line, RSTART + RLENGTH)
      }
    }
    next
  }
  function print_section() {
    if (heading == "" || heading ~ /cross-cutting/) return
    prio = "unspecified"
    if (match(body, /Priority:[^\n]*/)) {
      prio = substr(body, RSTART, RLENGTH)
    }
    blocked = 0
    if (body ~ /[Nn]o concrete trigger|[Nn]ot [Bb]locking|speculative/) blocked = 1
    itemNum = heading
    sub(/ .*/, "", itemNum)
    if (itemNum in globalBlocked) blocked = 1
    # A shipped/resolved item keeps its heading (the analysis is the historical
    # record) but must never be suggested as the next thing to build — without
    # this the suggestion walks straight into finished work whenever the top of
    # a tier has just been cleared.
    done = (heading ~ /SHIPPED|RESOLVED/)
    flag = done ? "  [SHIPPED]" : (blocked ? "  [NOT BLOCKING -- confirm with user before starting]" : "")
    printf("- %s\n    %s%s\n", heading, prio, flag)
    if (!blocked && !done && chosen == "") chosen = heading
  }
  /^### /{
    print_section()
    heading = $0
    sub(/^### /, "", heading)
    body = ""
    next
  }
  /^## /{
    print_section()
    heading = ""
  }
  { if (heading != "") body = body "\n" $0 }
  END {
    print_section()
    print ""
    print "=== Suggested next item (first non-blocked, document order) ==="
    if (chosen != "") print "-> " chosen
    else print "-> none found; all remaining items require a check-in with the user first"
  }
' "$ROADMAP" "$ROADMAP"
