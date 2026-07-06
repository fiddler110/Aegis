import type { JSX } from "preact";

export function Composer({
  value,
  onChange,
  disabled,
  streaming,
  onSend,
  onStop,
}: {
  value: string;
  onChange: (v: string) => void;
  disabled: boolean;
  streaming: boolean;
  onSend: () => void;
  onStop: () => void;
}) {
  const onKeyDown = (e: JSX.TargetedKeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (!streaming) onSend();
    }
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
