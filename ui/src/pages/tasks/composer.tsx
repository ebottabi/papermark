import { useState } from "react";
import { ArrowUp, Gauge, Sparkles } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Mode } from "@/types";
import { Textarea } from "@/components/ui";

export interface ComposerProps {
  onSend: (text: string, mode: Mode) => void;
  sending?: boolean;
  disabled?: boolean;
  placeholder?: string;
  autoFocus?: boolean;
  defaultMode?: Mode;
  showMode?: boolean;
}

/** The message input: textarea, a speed/quality toggle, and a send button. */
export const Composer = ({
  onSend,
  sending,
  disabled,
  placeholder = "Ask Keen to do something…",
  autoFocus,
  defaultMode = "quality",
  showMode = true,
}: ComposerProps) => {
  const [text, setText] = useState("");
  const [mode, setMode] = useState<Mode>(defaultMode);

  const submit = () => {
    const t = text.trim();
    if (!t || sending || disabled) return;
    onSend(t, mode);
    setText("");
  };

  return (
    <div
      className={cn(
        "rounded-xl border border-gray-300 bg-background-surface p-2 shadow-card focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/20",
        disabled && "pointer-events-none opacity-60",
      )}
    >
      <Textarea
        autoFocus={autoFocus}
        disabled={disabled}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={placeholder}
        rows={2}
        className="max-h-48 resize-none border-0 bg-transparent p-2 shadow-none focus:ring-0"
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            submit();
          }
        }}
      />
      <div className="flex items-center justify-between gap-2 px-1 pt-1">
        {showMode ? (
          <div className="flex rounded-lg bg-gray-100 p-0.5">
            {(["speed", "quality"] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => setMode(m)}
                className={cn(
                  "flex items-center gap-1.5 rounded-md px-2.5 py-1 text-body-3 font-medium capitalize transition-colors",
                  mode === m ? "bg-background-surface text-ink shadow-card" : "text-ink-muted hover:text-ink",
                )}
              >
                {m === "speed" ? <Gauge className="h-3.5 w-3.5" /> : <Sparkles className="h-3.5 w-3.5" />}
                {m}
              </button>
            ))}
          </div>
        ) : (
          <span />
        )}
        <button
          type="button"
          onClick={submit}
          disabled={!text.trim() || sending || disabled}
          aria-label="Send"
          className="flex h-8 w-8 items-center justify-center rounded-lg bg-ink text-white transition-colors hover:bg-ink-soft disabled:bg-gray-300"
        >
          <ArrowUp className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
};
