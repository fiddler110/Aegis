import { useState } from "preact/hooks";
import type { PersonaInfo } from "../types";

// SettingsPanel is the "Assistant" popover (P15.3): pick a persona (the
// assistant's role/behavior profile) and optionally pin a specific model for
// this chat. Personas are shown with their plain-language descriptions; the
// model override is tucked behind an "Advanced" section since most users
// should stay on the default. P15.9 adds a background-mode toggle there too:
// a background chat's responses keep running on the daemon even after every
// tab disconnects (buffered events let a reopened tab catch up).
export function SettingsPanel({
  personas,
  currentPersona,
  modelOverride,
  defaultModel,
  background,
  busy,
  onSwitchPersona,
  onSetModel,
  onSetBackground,
  onClose,
}: {
  personas: PersonaInfo[];
  currentPersona: string;
  modelOverride: string;
  defaultModel: string;
  background: boolean;
  busy: boolean;
  onSwitchPersona: (name: string) => void;
  onSetModel: (model: string) => void;
  onSetBackground: (on: boolean) => void;
  onClose: () => void;
}) {
  const [modelDraft, setModelDraft] = useState(modelOverride);
  const modelDirty = modelDraft.trim() !== modelOverride;

  return (
    <>
      <div class="backdrop" onClick={onClose} />
      <div class="panel" role="dialog" aria-label="Assistant settings">
        <div class="panel-head">
          <strong>Assistant</strong>
          <button class="linkish" onClick={onClose}>
            Close
          </button>
        </div>
        <p class="hint">
          Choose how the assistant behaves in this chat. Switching applies right away — the
          conversation so far is kept.
        </p>
        <div class="persona-list">
          {personas.map((p) => {
            const active = p.name === currentPersona;
            return (
              <button
                key={p.name}
                class={"persona-row" + (active ? " active" : "")}
                disabled={busy || active}
                onClick={() => onSwitchPersona(p.name)}
              >
                <span class="pr-name">
                  {active ? "✓ " : ""}
                  {p.name}
                </span>
                <span class="pr-desc">{p.description || "No description"}</span>
              </button>
            );
          })}
          {personas.length === 0 && <p class="hint">No personas available.</p>}
        </div>
        <details class="advanced">
          <summary>Advanced: use a different model for this chat</summary>
          <p class="hint">
            Normally the default model{defaultModel ? ` (${defaultModel})` : ""} is used. Enter a
            model name your provider offers to override it for this chat only.
          </p>
          <div class="model-row">
            <input
              type="text"
              placeholder={defaultModel ? `default (${defaultModel})` : "default model"}
              value={modelDraft}
              disabled={busy}
              onInput={(e) => setModelDraft((e.target as HTMLInputElement).value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && modelDirty) onSetModel(modelDraft.trim());
              }}
            />
            <button disabled={busy || !modelDirty} onClick={() => onSetModel(modelDraft.trim())}>
              Apply
            </button>
            {modelOverride && (
              <button
                class="secondary"
                disabled={busy}
                onClick={() => {
                  setModelDraft("");
                  onSetModel("");
                }}
              >
                Use default
              </button>
            )}
          </div>
        </details>
        <details class="advanced">
          <summary>Advanced: keep this chat working in the background</summary>
          <p class="hint">
            When on, the assistant keeps working on its answer even if you close this tab. Come
            back later and the chat catches up on what happened while you were away.
          </p>
          <label class="bg-toggle">
            <input
              type="checkbox"
              checked={background}
              disabled={busy}
              onChange={(e) => onSetBackground((e.target as HTMLInputElement).checked)}
            />
            Keep working when this tab is closed
          </label>
        </details>
      </div>
    </>
  );
}
