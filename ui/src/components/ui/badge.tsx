import { cn } from "@/lib/utils";
import { toneClasses, type Tone } from "./variants";
import type { Status } from "@/types";

export interface BadgeProps {
  tone?: Tone;
  dot?: boolean;
  className?: string;
  children: React.ReactNode;
}

export const Badge = ({ tone = "neutral", dot, className, children }: BadgeProps) => {
  const t = toneClasses[tone];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-body-4 font-medium",
        t.bg,
        t.border,
        t.text,
        className,
      )}
    >
      {dot ? <span className={cn("h-1.5 w-1.5 rounded-full", t.dot)} /> : null}
      {children}
    </span>
  );
};

const STATUS_TONE: Record<Status, Tone> = {
  running: "accent",
  waiting: "high",
  done: "forest",
  stopped: "neutral",
  error: "crit",
};

const STATUS_LABEL: Record<Status, string> = {
  running: "Running",
  waiting: "Waiting",
  done: "Done",
  stopped: "Stopped",
  error: "Error",
};

export const StatusBadge = ({ status }: { status: Status }) => (
  <Badge tone={STATUS_TONE[status]} dot>
    {STATUS_LABEL[status]}
  </Badge>
);

const CONNECTOR_TONE = { connected: "forest", needs_auth: "high", error: "crit" } as const;

export const ConnectorStatusBadge = ({ status }: { status: keyof typeof CONNECTOR_TONE }) => (
  <Badge tone={CONNECTOR_TONE[status]} dot>
    {status === "needs_auth" ? "Needs auth" : status === "error" ? "Error" : "Connected"}
  </Badge>
);
