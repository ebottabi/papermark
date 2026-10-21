import { Users } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge, Spinner } from "@/components/ui";
import { useTaskStream } from "@/hooks/use-task-stream";
import type { AgentLane as Lane } from "../fold-timeline";
import { Markdown } from "./markdown";

// The child's latest spoken line, or what it is doing if it hasn't spoken yet.
function liveStatus(events: { event: { type: string; text?: string } }[], activity: string | null): string {
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i].event;
    if ((e.type === "notify" || e.type === "ask") && e.text) return e.text;
  }
  return activity ?? "Starting…";
}

// Each lane calls useTaskStream once at its top level, so lanes must be their
// own component — the hook can't run in a loop (React rules-of-hooks).
const AgentLane = ({ lane }: { lane: Lane }) => {
  // Stop streaming the child once it has finished; its result is persisted here.
  const stream = useTaskStream(lane.result ? undefined : lane.agentId);
  const done = lane.result;

  return (
    <div className={cn("rounded-lg border px-3 py-2.5", done?.isError ? "border-crit-border bg-crit-soft" : "border-gray-200")}>
      <div className="flex items-center gap-2">
        <span className="min-w-0 truncate text-body-3 font-medium text-ink">{lane.title}</span>
        {done ? (
          <Badge tone={done.isError ? "crit" : "forest"} className="ml-auto">
            {done.isError ? "Failed" : "Done"}
          </Badge>
        ) : (
          <span className="ml-auto flex shrink-0 items-center gap-1.5 text-body-4 text-ink-faint">
            <Spinner className="h-3.5 w-3.5" /> Running
          </span>
        )}
      </div>
      {done ? (
        done.text ? <Markdown className="mt-1.5">{done.text}</Markdown> : null
      ) : (
        <p className="mt-1 truncate text-body-3 text-ink-muted">{liveStatus(stream.events, stream.activity)}</p>
      )}
    </div>
  );
};

export const AgentLanes = ({ lanes }: { lanes: Lane[] }) => (
  <div className="ml-9 flex flex-col gap-2">
    <div className="flex items-center gap-1.5 text-body-4 font-medium text-ink-muted">
      <Users className="h-3.5 w-3.5" />
      Sub-agents
    </div>
    {lanes.map((lane) => (
      <AgentLane key={lane.agentId} lane={lane} />
    ))}
  </div>
);
