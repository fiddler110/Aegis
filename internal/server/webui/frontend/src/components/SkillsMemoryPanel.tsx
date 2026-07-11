import { useEffect, useState } from "preact/hooks";
import { api } from "../api";
import type { BuiltinSkillInfo, ConfigSkillsResponse, MemoryResponse } from "../types";

type Scope = "project" | "global";

// SkillsMemoryPanel is the "Skills & memory" popover (P15.7), in two tabs:
//
//  - Memory: what the assistant remembers for this project and across all
//    projects (GET /memory), each with an "Add a note" composer
//    (POST /memory — returns 204, so no body to parse).
//  - Playbooks: on/off toggles for the built-in playbooks that ship inside
//    Aegis (GET/PATCH /config/skills). The PATCH is a full replace of the
//    enabled list, so we always send the complete desired set; names enabled
//    outside the catalog (e.g. typed via the CLI) are preserved untouched.
export function SkillsMemoryPanel({
  onClose,
  addToast,
}: {
  onClose: () => void;
  addToast: (text: string, warn?: boolean) => void;
}) {
  const [tab, setTab] = useState<"memory" | "playbooks">("memory");

  // ── Memory tab state ──
  const [mem, setMem] = useState<MemoryResponse | null>(null);
  const [memErr, setMemErr] = useState<string | null>(null);
  const [projectNote, setProjectNote] = useState("");
  const [userNote, setUserNote] = useState("");
  const [savingNote, setSavingNote] = useState<"project" | "user" | null>(null);

  // ── Playbooks tab state ──
  const [scope, setScope] = useState<Scope>("project");
  const [catalog, setCatalog] = useState<BuiltinSkillInfo[]>([]);
  const [enabled, setEnabled] = useState<Set<string>>(new Set());
  const [baseline, setBaseline] = useState<Set<string>>(new Set());
  const [skillsErr, setSkillsErr] = useState<string | null>(null);
  const [skillsLoading, setSkillsLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  const loadMemory = async () => {
    try {
      setMemErr(null);
      const r = await api("/memory");
      setMem((await r.json()) as MemoryResponse);
    } catch (e) {
      setMemErr((e as Error).message);
    }
  };

  const loadSkills = async (sc: Scope) => {
    setSkillsLoading(true);
    setSkillsErr(null);
    try {
      const r = await api(`/config/skills?scope=${sc}`);
      const body = (await r.json()) as ConfigSkillsResponse;
      setCatalog(body.available || []);
      setEnabled(new Set(body.builtin_enabled || []));
      setBaseline(new Set(body.builtin_enabled || []));
    } catch (e) {
      setSkillsErr((e as Error).message);
    } finally {
      setSkillsLoading(false);
    }
  };

  useEffect(() => {
    loadMemory();
    loadSkills(scope);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const addNote = async (noteScope: "project" | "user") => {
    const entry = (noteScope === "project" ? projectNote : userNote).trim();
    if (!entry || savingNote) return;
    setSavingNote(noteScope);
    try {
      // POST /memory replies 204 No Content — nothing to parse on success.
      await api("/memory", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ entry, scope: noteScope }),
      });
      if (noteScope === "project") setProjectNote("");
      else setUserNote("");
      await loadMemory();
      addToast(
        noteScope === "project"
          ? "Noted — the assistant will remember that for this project."
          : "Noted — the assistant will remember that in every project."
      );
    } catch (e) {
      addToast("Could not save the note: " + (e as Error).message, true);
    } finally {
      setSavingNote(null);
    }
  };

  const switchScope = (sc: Scope) => {
    if (sc === scope || skillsLoading || saving) return;
    setScope(sc);
    loadSkills(sc); // discards unsaved toggles — the list reloads from disk
  };

  const toggle = (name: string) => {
    setEnabled((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const dirty =
    enabled.size !== baseline.size || [...enabled].some((n) => !baseline.has(n));

  const save = async () => {
    if (saving || !dirty) return;
    setSaving(true);
    setSkillsErr(null);
    try {
      // Full-replace semantics: always send the complete desired set.
      const r = await api("/config/skills", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ scope, builtin_enabled: [...enabled].sort() }),
      });
      const body = (await r.json()) as ConfigSkillsResponse;
      setCatalog(body.available || []);
      setEnabled(new Set(body.builtin_enabled || []));
      setBaseline(new Set(body.builtin_enabled || []));
      addToast("Playbook settings saved.");
      loadMemory(); // the "usable right now" list may have changed
    } catch (e) {
      addToast("Could not save playbook settings: " + (e as Error).message, true);
    } finally {
      setSaving(false);
    }
  };

  const activeSkills = mem?.skills || [];

  return (
    <>
      <div class="backdrop" onClick={onClose} />
      <div class="panel wide" role="dialog" aria-label="Skills and memory">
        <div class="panel-head">
          <strong>Skills &amp; memory</strong>
          <button class="linkish" onClick={onClose}>
            Close
          </button>
        </div>
        <div class="sm-tabs">
          <button
            class={"tab" + (tab === "memory" ? " active" : "")}
            onClick={() => setTab("memory")}
          >
            What it remembers
          </button>
          <button
            class={"tab" + (tab === "playbooks" ? " active" : "")}
            onClick={() => setTab("playbooks")}
          >
            Playbooks
          </button>
        </div>

        {tab === "memory" && (
          <div>
            <p class="hint">
              Notes the assistant keeps between chats and quietly consults when they seem relevant.
            </p>
            {memErr && <p class="err">{memErr}</p>}

            <h4 class="sm-h">This project</h4>
            {mem && mem.project_memory ? (
              <div class="sm-mem">{mem.project_memory}</div>
            ) : (
              <p class="hint">Nothing remembered for this project yet.</p>
            )}
            <div class="kb-search">
              <input
                type="text"
                placeholder="Add a note about this project…"
                value={projectNote}
                disabled={savingNote !== null}
                onInput={(e) => setProjectNote((e.target as HTMLInputElement).value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") addNote("project");
                }}
              />
              <button
                disabled={savingNote !== null || !projectNote.trim()}
                onClick={() => addNote("project")}
              >
                {savingNote === "project" ? "Saving…" : "Add"}
              </button>
            </div>

            <h4 class="sm-h">Every project</h4>
            {mem && mem.user_memory ? (
              <div class="sm-mem">{mem.user_memory}</div>
            ) : (
              <p class="hint">Nothing remembered across projects yet.</p>
            )}
            <div class="kb-search">
              <input
                type="text"
                placeholder="Add a note the assistant should remember everywhere…"
                value={userNote}
                disabled={savingNote !== null}
                onInput={(e) => setUserNote((e.target as HTMLInputElement).value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") addNote("user");
                }}
              />
              <button
                disabled={savingNote !== null || !userNote.trim()}
                onClick={() => addNote("user")}
              >
                {savingNote === "user" ? "Saving…" : "Add"}
              </button>
            </div>
          </div>
        )}

        {tab === "playbooks" && (
          <div>
            <p class="hint">
              Playbooks are step-by-step abilities the assistant can use — some are written by you
              or your team, and some ship built in but stay off until you turn them on.
            </p>

            <h4 class="sm-h">Playbooks the assistant can use right now</h4>
            {activeSkills.length ? (
              <div class="sm-chips">
                {activeSkills.map((name) => (
                  <span class="badge" key={name}>
                    {name}
                  </span>
                ))}
              </div>
            ) : (
              <p class="hint">None yet — turn on a built-in below, or add a skill file.</p>
            )}

            <h4 class="sm-h">Built-in playbooks</h4>
            <div class="sm-scope">
              <span class="hint">Turn on for:</span>
              <button
                class={"chip" + (scope === "project" ? " on" : "")}
                disabled={skillsLoading || saving}
                onClick={() => switchScope("project")}
              >
                This project
              </button>
              <button
                class={"chip" + (scope === "global" ? " on" : "")}
                disabled={skillsLoading || saving}
                onClick={() => switchScope("global")}
              >
                All projects
              </button>
            </div>
            {skillsErr && <p class="err">{skillsErr}</p>}
            {skillsLoading && <p class="hint">Loading…</p>}
            {!skillsLoading &&
              catalog.map((sk) => (
                <label class="sm-skill" key={sk.name}>
                  <input
                    type="checkbox"
                    checked={enabled.has(sk.name)}
                    disabled={saving}
                    onChange={() => toggle(sk.name)}
                  />
                  <span>
                    <span class="n">{sk.name}</span>
                    {sk.description && <span class="d">{sk.description}</span>}
                  </span>
                </label>
              ))}
            <div class="sm-save">
              <button disabled={saving || !dirty} onClick={save}>
                {saving ? "Saving…" : "Save changes"}
              </button>
              <span class="hint">Changes take effect from the assistant's next response.</span>
            </div>
          </div>
        )}
      </div>
    </>
  );
}
