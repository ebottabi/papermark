import { useState } from "react";
import { ShieldQuestion } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui";
import type { Decision } from "@/types";
import type { Block } from "../fold-timeline";
import { toolMeta } from "../tool-meta";

type Confirm = Extract<Block, { kind: "confirm" }>;

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
}
function val(r: Record<string, unknown>, keys: string[]): string {
  for (const k of keys) {
    const v = r[k];
    if (typeof v === "string" && v.trim()) return v;
    if (typeof v === "number" || typeof v === "boolean") return String(v);
  }
  return "";
}

function isEmail(name: string, r: Record<string, unknown>): boolean {
  return (/gmail|email|mail/i.test(name) || "to" in r || "recipient" in r) && ("subject" in r || "body" in r || "to" in r);
}
function isCalendar(name: string, r: Record<string, unknown>): boolean {
  return /calendar|event/i.test(name) || "start" in r || "start_time" in r || "when" in r;
}

const Field = ({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) => (
  <div className="flex gap-2 text-body-3">
    <span className="w-16 shrink-0 text-ink-faint">{label}</span>
    <span className={cn("min-w-0 flex-1 break-words text-ink", mono && "font-mono")}>{children}</span>
  </div>
);

const Preview = ({ name, input }: { name: string; input: unknown }) => {
  const [showFull, setShowFull] = useState(false);
  const r = rec(input);

  if (isEmail(name, r)) {
    const body = val(r, ["body", "text", "message"]);
    const clipped = body.length > 240 && !showFull;
    return (
      <div className="space-y-1.5 rounded-md border border-gray-200 bg-background-surface p-3">
        {val(r, ["from"]) ? <Field label="From">{val(r, ["from"])}</Field> : null}
        <Field label="To">{val(r, ["to", "recipient"])}</Field>
        <Field label="Subject">{val(r, ["subject"])}</Field>
        {body ? (
          <div className="mt-1 whitespace-pre-wrap border-t border-gray-200 pt-2 text-body-3 text-ink">
            {clipped ? `${body.slice(0, 240)}…` : body}
            {body.length > 240 ? (
              <button
                type="button"
                onClick={() => setShowFull((s) => !s)}
                className="ml-1 font-medium text-accent-ink underline"
              >
                {showFull ? "Show less" : "Show full message"}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    );
  }

  if (isCalendar(name, r)) {
    return (
      <div className="space-y-1.5 rounded-md border border-gray-200 bg-background-surface p-3">
        <Field label="Event">{val(r, ["summary", "title", "name"])}</Field>
        {val(r, ["start", "start_time", "when"]) ? <Field label="Start">{val(r, ["start", "start_time", "when"])}</Field> : null}
        {val(r, ["end", "end_time"]) ? <Field label="End">{val(r, ["end", "end_time"])}</Field> : null}
        {val(r, ["location"]) ? <Field label="Where">{val(r, ["location"])}</Field> : null}
      </div>
    );
  }

  const entries = Object.entries(r).filter(([, v]) => v != null && v !== "");
  if (!entries.length) return null;
  return (
    <div className="space-y-1.5 rounded-md border border-gray-200 bg-background-surface p-3">
      {entries.map(([k, v]) => (
        <Field key={k} label={k} mono>
          {typeof v === "string" ? v : JSON.stringify(v)}
        </Field>
      ))}
    </div>
  );
};

function question(name: string, r: Record<string, unknown>): string {
  if (isEmail(name, r)) return "Send this email from Gmail?";
  if (name === "shell_exec") return "Run this command?";
  if (isCalendar(name, r)) return "Add this event to your calendar?";
  return `Allow ${toolMeta(name, undefined).title.toLowerCase()}?`;
}

export const ApprovalCard = ({
  confirm,
  active,
  busy,
  onDecide,
}: {
  confirm: Confirm;
  active: boolean;
  busy: boolean;
  onDecide: (decision: Decision) => void;
}) => {
  if (confirm.decision !== null) {
    return (
      <p className="ml-9 text-body-3 text-ink-faint">
        You {confirm.decision === "deny" ? "denied" : "allowed"} this.
      </p>
    );
  }
  if (!active) {
    return (
      <p className="ml-9 text-body-3 text-ink-faint">Waiting for your approval.</p>
    );
  }

  const r = rec(confirm.input);
  const email = isEmail(confirm.name, r);
  const primaryLabel = email ? "Send" : "Allow";

  return (
    <div className="ml-9 rounded-lg border border-high-border bg-high-soft p-4">
      <div className="mb-2.5 flex items-center gap-2">
        <ShieldQuestion className="h-4 w-4 text-high" />
        <p className="text-body-2 font-medium text-ink">{question(confirm.name, r)}</p>
      </div>
      <Preview name={confirm.name} input={confirm.input} />
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button size="sm" variant="accent" onClick={() => onDecide("allow")} disabled={busy}>
          {primaryLabel}
        </Button>
        <Button size="sm" variant="secondary" onClick={() => onDecide("deny")} disabled={busy}>
          Deny
        </Button>
        {confirm.allowAlways && !email ? (
          <button
            type="button"
            onClick={() => onDecide("always")}
            disabled={busy}
            className="ml-auto text-body-3 text-ink-faint underline underline-offset-2 hover:text-ink-muted disabled:opacity-50"
          >
            Always allow
          </button>
        ) : null}
      </div>
    </div>
  );
};
