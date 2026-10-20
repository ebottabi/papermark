import { useState } from "react";
import { ChevronUp, Square } from "lucide-react";
import { cn } from "@/lib/utils";
import { Spinner, ConfirmDialog } from "@/components/ui";
import type { Status } from "@/types";
import type { Plan } from "../fold-timeline";
import { PlanChecklist } from "./plan-checklist";

export type WaitingKind = "reply" | "approval" | null;

function phaseText(
  status: Status,
  runningStepTitle: string | undefined,
  waiting: WaitingKind,
  failureText: string,
): string {
  switch (status) {
    case "running":
      return runningStepTitle || "Keen is working";
    case "waiting":
      return waiting === "approval" ? "Waiting for your approval" : "Waiting for your reply";
    case "done":
      return "Task completed";
    case "stopped":
      return "Task stopped";
    case "error":
      return failureText || "The task hit an error";
  }
}

const Mark = ({ status }: { status: Status }) => {
  if (status === "running") return <Spinner className="h-3.5 w-3.5" />;
  const dot =
    status === "waiting"
      ? "bg-high"
      : status === "done"
        ? "bg-forest"
        : status === "error"
          ? "bg-crit"
          : "bg-gray-500";
  return <span className={cn("h-2 w-2 rounded-full", dot, status === "waiting" && "animate-blink")} />;
};

export const StatusStrip = ({
  status,
  plan,
  runningStepTitle,
  waiting,
  failureText,
  stopping,
  onStop,
}: {
  status: Status;
  plan: Plan | null;
  runningStepTitle?: string;
  waiting: WaitingKind;
  failureText: string;
  stopping: boolean;
  onStop: () => void;
}) => {
  const [expanded, setExpanded] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);
  const canStop = status === "running" || status === "waiting";
  const textColor =
    status === "error" ? "text-crit" : status === "done" ? "text-forest" : status === "waiting" ? "text-high" : "text-ink";

  return (
    <div className="mb-2 rounded-lg border border-gray-200 bg-background-surface">
      <div className="flex items-center gap-2.5 px-3 py-2">
        <Mark status={status} />
        <span className={cn("min-w-0 truncate text-body-3 font-medium", textColor)}>
          {phaseText(status, runningStepTitle, waiting, failureText)}
        </span>

        {plan ? (
          <button
            type="button"
            onClick={() => setExpanded((e) => !e)}
            className="ml-auto flex items-center gap-1.5 rounded-md px-1.5 py-0.5 text-ink-faint hover:bg-gray-100 hover:text-ink-muted"
            aria-label={expanded ? "Hide plan" : "Show plan"}
          >
            <span className="font-mono text-body-4">
              {plan.done}/{plan.total}
            </span>
            <ChevronUp className={cn("h-3.5 w-3.5 transition-transform", !expanded && "rotate-180")} />
          </button>
        ) : null}

        {canStop ? (
          <button
            type="button"
            onClick={() => setConfirmStop(true)}
            disabled={stopping}
            aria-label="Stop task"
            className={cn(
              "flex h-7 w-7 items-center justify-center rounded-full border border-gray-300 text-ink-muted hover:border-crit-border hover:bg-crit-soft hover:text-crit disabled:opacity-50",
              !plan && "ml-auto",
            )}
          >
            {stopping ? <Spinner className="h-3.5 w-3.5" /> : <Square className="h-3 w-3 fill-current" />}
          </button>
        ) : null}
      </div>

      {plan && expanded ? (
        <div className="border-t border-gray-200 px-3 py-2.5">
          <PlanChecklist plan={plan} />
        </div>
      ) : null}

      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title="Stop this task?"
        description="Keen stops after the current step; work so far is kept."
        confirmLabel="Stop"
        destructive
        loading={stopping}
        onConfirm={() => {
          onStop();
          setConfirmStop(false);
        }}
      />
    </div>
  );
};
