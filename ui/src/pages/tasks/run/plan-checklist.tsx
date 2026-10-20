import { Check } from "lucide-react";
import { cn } from "@/lib/utils";
import { Spinner } from "@/components/ui";
import type { Plan } from "../fold-timeline";

const Mark = ({ status }: { status: "done" | "running" | "pending" }) => {
  if (status === "done")
    return (
      <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-forest-soft text-forest">
        <Check className="h-3 w-3" strokeWidth={3} />
      </span>
    );
  if (status === "running") return <Spinner className="h-4 w-4 shrink-0" />;
  return <span className="h-4 w-4 shrink-0 rounded-full border border-gray-400" />;
};

export const PlanChecklist = ({ plan }: { plan: Plan }) => (
  <ul className="space-y-1.5">
    {plan.steps.map((step, i) => (
      <li key={i} className="flex items-start gap-2.5 text-body-3">
        <span className="mt-0.5">
          <Mark status={step.status} />
        </span>
        <span
          className={cn(
            "leading-snug",
            step.status === "done" && "text-ink-faint line-through",
            step.status === "running" && "font-medium text-ink",
            step.status === "pending" && "text-ink-muted",
          )}
        >
          {step.title}
        </span>
      </li>
    ))}
  </ul>
);
