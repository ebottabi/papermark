import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Inbox, Plus, Search } from "lucide-react";
import { tasksApi } from "@/api/tasks";
import { ApiError } from "@/lib/http";
import { cn } from "@/lib/utils";
import { ROUTES } from "@/lib/constants";
import { Button, EmptyState, InlineError, Input, SkeletonRows, toast } from "@/components/ui";
import type { TaskSummary } from "@/types";
import { TaskRow } from "./task-row";

type Group = { key: string; label: string; tasks: TaskSummary[] };

const DAY = 86_400_000;

// Buckets by updatedAt; caller passes a list already sorted most-recent first.
const groupByDay = (tasks: TaskSummary[]): Group[] => {
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const buckets: Record<string, TaskSummary[]> = { today: [], yesterday: [], week: [], earlier: [] };
  for (const t of tasks) {
    const u = new Date(t.updatedAt).getTime();
    if (u >= today) buckets.today.push(t);
    else if (u >= today - DAY) buckets.yesterday.push(t);
    else if (u >= today - 6 * DAY) buckets.week.push(t);
    else buckets.earlier.push(t);
  }
  const order: [string, string][] = [
    ["today", "Today"],
    ["yesterday", "Yesterday"],
    ["week", "This week"],
    ["earlier", "Earlier"],
  ];
  return order.filter(([k]) => buckets[k].length).map(([key, label]) => ({ key, label, tasks: buckets[key] }));
};

const SectionHeader = ({ children }: { children: React.ReactNode }) => (
  <h2 className="px-3 pb-1 pt-3 text-body-4 font-semibold uppercase tracking-wide text-ink-faint first:pt-1">
    {children}
  </h2>
);

export const Rail = ({ onNavigate }: { onNavigate?: () => void }) => {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [q, setQ] = useState("");
  const [archived, setArchived] = useState(false);
  const tasksKey = ["tasks", { archived }] as const;

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: tasksKey,
    queryFn: () => tasksApi.list({ archived }),
    refetchInterval: 8000,
  });

  const favorite = useMutation({
    mutationFn: (t: TaskSummary) => tasksApi.patch(t.id, { favorite: !t.favorite }),
    onMutate: async (t) => {
      await qc.cancelQueries({ queryKey: tasksKey });
      const prev = qc.getQueryData<TaskSummary[]>(tasksKey);
      qc.setQueryData<TaskSummary[]>(tasksKey, (list) =>
        list?.map((x) => (x.id === t.id ? { ...x, favorite: !t.favorite } : x)),
      );
      return { prev };
    },
    onError: (e, _t, ctx) => {
      if (ctx?.prev) qc.setQueryData(tasksKey, ctx.prev);
      toast.error("Couldn't update favorite", e instanceof ApiError ? e.message : undefined);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["tasks"] }),
  });

  const archive = useMutation({
    mutationFn: (t: TaskSummary) => tasksApi.patch(t.id, { archived: !t.archived }),
    onMutate: async (t) => {
      await qc.cancelQueries({ queryKey: tasksKey });
      const prev = qc.getQueryData<TaskSummary[]>(tasksKey);
      qc.setQueryData<TaskSummary[]>(tasksKey, (list) => list?.filter((x) => x.id !== t.id));
      return { prev };
    },
    onError: (e, _t, ctx) => {
      if (ctx?.prev) qc.setQueryData(tasksKey, ctx.prev);
      toast.error("Couldn't archive task", e instanceof ApiError ? e.message : undefined);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["tasks"] }),
  });

  const query = q.trim().toLowerCase();
  const shown = (data ?? [])
    .filter((t) => t.prompt.toLowerCase().includes(query))
    .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));

  // Float blocked tasks to the top of the active view; they are the highest-signal state.
  const needsYou = archived ? [] : shown.filter((t) => t.status === "waiting");
  const groups = groupByDay(archived ? shown : shown.filter((t) => t.status !== "waiting"));

  const tab = (label: string, value: boolean) => (
    <button
      type="button"
      onClick={() => setArchived(value)}
      className={cn(
        "rounded-md px-2 py-1 text-body-4 font-medium transition-colors",
        archived === value ? "bg-gray-100 text-ink" : "text-ink-faint hover:text-ink-muted",
      )}
    >
      {label}
    </button>
  );

  return (
    <div className="flex h-full w-full flex-col border-r border-gray-200 bg-background-surface sm:w-[300px]">
      <div className="space-y-3 border-b border-gray-200 p-3">
        <button
          type="button"
          onClick={() => {
            navigate(ROUTES.TASKS);
            onNavigate?.();
          }}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-accent px-3 py-2 text-body-2 font-medium text-white transition-colors hover:bg-accent-hover"
        >
          <Plus className="h-4 w-4" /> New task
        </button>
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-faint" />
          <Input
            inputSize="sm"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search tasks"
            className="pl-8"
          />
        </div>
        <div className="flex items-center gap-1">
          {tab("Active", false)}
          {tab("Archived", true)}
        </div>
      </div>

      <div className="scrollbar-thin flex-1 overflow-y-auto p-2">
        {isLoading ? (
          <SkeletonRows rows={6} />
        ) : isError ? (
          <InlineError message="Couldn't load tasks." onRetry={refetch} />
        ) : shown.length === 0 ? (
          query ? (
            <p className="px-3 py-8 text-center text-body-3 text-ink-faint">No tasks match “{q}”.</p>
          ) : archived ? (
            <EmptyState icon={<Inbox className="h-5 w-5" />} title="No archived tasks" description="Tasks you archive land here." />
          ) : (
            <EmptyState
              icon={<Inbox className="h-5 w-5" />}
              title="No tasks yet"
              description="Start a task and Keen gets to work. It shows up here."
              action={
                <Button variant="accent" size="sm" leftIcon={<Plus className="h-4 w-4" />} onClick={() => { navigate(ROUTES.TASKS); onNavigate?.(); }}>
                  New task
                </Button>
              }
            />
          )
        ) : (
          <>
            {needsYou.length ? (
              <section>
                <SectionHeader>Needs you</SectionHeader>
                <ul className="space-y-0.5">
                  {needsYou.map((t) => (
                    <TaskRow key={t.id} task={t} onNavigate={onNavigate} onToggleFavorite={favorite.mutate} onToggleArchive={archive.mutate} />
                  ))}
                </ul>
              </section>
            ) : null}
            {groups.map((g) => (
              <section key={g.key}>
                <SectionHeader>{g.label}</SectionHeader>
                <ul className="space-y-0.5">
                  {g.tasks.map((t) => (
                    <TaskRow key={t.id} task={t} onNavigate={onNavigate} onToggleFavorite={favorite.mutate} onToggleArchive={archive.mutate} />
                  ))}
                </ul>
              </section>
            ))}
          </>
        )}
      </div>
    </div>
  );
};
