import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { tasksApi } from "@/api/tasks";
import { ROUTES } from "@/lib/constants";
import { ApiError } from "@/lib/http";
import type { Mode } from "@/types";
import { toast } from "@/components/ui";
import { Composer } from "./composer";

const SUGGESTIONS = [
  "Summarize my unread email and flag anything urgent",
  "Research the latest on our top three competitors",
  "Draft a reply to the last message from my manager",
  "Watch this page and tell me when the price drops",
];

const TaskStart = () => {
  const navigate = useNavigate();
  const qc = useQueryClient();

  const create = useMutation({
    mutationFn: ({ text, mode }: { text: string; mode: Mode }) => tasksApi.create({ task: text, mode }),
    onSuccess: ({ id }) => {
      qc.invalidateQueries({ queryKey: ["tasks"] });
      navigate(`${ROUTES.TASKS}/${id}`);
    },
    onError: (err) => toast.error("Couldn't start task", err instanceof ApiError ? err.message : undefined),
  });

  return (
    <div className="flex flex-1 flex-col items-center justify-center overflow-y-auto px-4 py-10">
      <div className="w-full max-w-2xl">
        <div className="mb-6 text-center">
          <h1 className="text-page-header font-semibold tracking-tight text-ink">What should Keen do?</h1>
          <p className="mt-2 text-body-2 text-ink-muted">
            Describe a task. Keen runs it end to end and checks in when it needs you.
          </p>
        </div>

        <Composer
          autoFocus
          sending={create.isPending}
          onSend={(text, mode) => create.mutate({ text, mode })}
        />

        <div className="mt-6 flex flex-wrap justify-center gap-2">
          {SUGGESTIONS.map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => create.mutate({ text: s, mode: "quality" })}
              disabled={create.isPending}
              className="rounded-full border border-gray-300 bg-background-surface px-3 py-1.5 text-body-3 text-ink-muted transition-colors hover:border-accent-border hover:bg-accent-soft hover:text-accent-ink disabled:opacity-50"
            >
              {s}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
};

export default TaskStart;
