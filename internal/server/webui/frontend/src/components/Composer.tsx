import type { JSX } from "preact";
import { useState } from "preact/hooks";
import type { DriveSkillInfo } from "../types";

export function Composer({
  value,
  onChange,
  disabled,
  streaming,
  driveSkills,
  onSend,
  onDrive,
  onStop,
}: {
  value: string;
  onChange: (v: string) => void;
  disabled: boolean;
  streaming: boolean;
  // driveSkills are the skills this session can drive to completion in phased
  // mode (P52.12). Empty hides the control entirely rather than offering a
  // button that can only fail.
  driveSkills: DriveSkillInfo[];
  onSend: () => void;
  onDrive: (skill: string) => void;
  onStop: () => void;
}) {
  const [pickerOpen, setPickerOpen] = useState(false);

  const onKeyDown = (e: JSX.TargetedKeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (!streaming) onSend();
    }
  };

  const startDrive = (skill: string) => {
    setPickerOpen(false);
    onDrive(skill);
  };

  return (
    <div id="composer">
      <textarea
        id="input"
        placeholder="Send a message…  (Enter to send, Shift+Enter for newline)"
        disabled={disabled}
        value={value}
        onInput={(e) => onChange((e.target as HTMLTextAreaElement).value)}
        onKeyDown={onKeyDown}
      />
      {!streaming && driveSkills.length > 0 && (
        <div class="drive-wrap">
          <button
            class="secondary"
            disabled={disabled || !value.trim()}
            title="Run a skill's phased build to completion: one fresh context per phase, auto verify, resumes from disk. Survives closing this tab."
            onClick={() => setPickerOpen((o) => !o)}
          >
            Drive ▾
          </button>
          {pickerOpen && (
            <div class="drive-menu">
              <p class="hint">
                Drive a phased build with the composer text as the task. It keeps going without
                stopping between phases, and survives closing this tab.
              </p>
              {driveSkills.map((s) => (
                <button key={s.name} class="drive-item" onClick={() => startDrive(s.name)}>
                  <strong>{s.name}</strong>
                  <span class="hint">
                    {s.phases} phases{s.description ? " — " + s.description : ""}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}
      <button
        id="send"
        class={streaming ? "stop" : undefined}
        disabled={disabled}
        onClick={() => (streaming ? onStop() : onSend())}
      >
        {streaming ? "Stop" : "Send"}
      </button>
    </div>
  );
}
