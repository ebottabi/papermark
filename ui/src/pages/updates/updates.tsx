import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, CheckCheck } from "lucide-react";
import { updatesApi } from "@/api/library";
import { cn, timeAgo } from "@/lib/utils";
import type { Importance, Update } from "@/types";
import { Badge, Button, EmptyState, InlineError, PageHeader, SkeletonRows } from "@/components/ui";

const TONE: Record<Importance, { tone: "crit" | "accent" | "neutral"; label: string }> = {
  push: { tone: "crit", label: "Push" },
  feed: { tone: "accent", label: "Feed" },
  quiet: { tone: "neutral", label: "Quiet" },
};

const UpdateCard = ({ update, onRead }: { update: Update; onRead: (id: string) => void }) => (
  <button
    type="button"
    onClick={() => !update.read && onRead(update.id)}
    className={cn(
      "w-full rounded-xl border p-4 text-left transition-colors",
      update.read ? "border-gray-200 bg-background-surface" : "border-accent-border bg-accent-soft",
    )}
  >
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          {!update.read ? <span className="h-2 w-2 shrink-0 rounded-full bg-accent" /> : null}
          <h3 className="truncate text-body-1 font-medium text-ink">{update.title}</h3>
        </div>
        <p className="mt-1 line-clamp-2 text-body-2 text-ink-muted">{update.body}</p>
      </div>
      <Badge tone={TONE[update.importance].tone}>{TONE[update.importance].label}</Badge>
    </div>
    {update.sections.length ? (
      <div className="mt-3 space-y-2 border-t border-gray-200/70 pt-3">
        {update.sections.map((s, i) => (
          <div key={i}>
            <p className="text-body-3 font-medium text-ink">{s.title}</p>
            <ul className="mt-1 space-y-0.5">
              {s.items.map((it, j) => (
                <li key={j} className="text-body-3 text-ink-muted">
                  {it.url ? (
                    <a href={it.url} target="_blank" rel="noreferrer" className="text-accent hover:underline">
                      {it.text}
                    </a>
                  ) : (
                    it.text
                  )}
                  {it.detail ? <span className="text-ink-faint"> — {it.detail}</span> : null}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    ) : null}
    <p className="mt-3 text-body-4 text-ink-faint">{timeAgo(update.createdAt)}</p>
  </button>
);

const Updates = () => {
  const qc = useQueryClient();
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ["updates"],
    queryFn: () => updatesApi.list({ limit: 50 }),
  });

  const read = useMutation({
    mutationFn: (id: string) => updatesApi.read(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["updates"] }),
  });
  const readAll = useMutation({
    mutationFn: () => updatesApi.readAll(),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["updates"] }),
  });

  const unread = (data ?? []).filter((u) => !u.read).length;

  return (
    <div>
      <PageHeader
        title="Updates"
        description="What Keen has done and surfaced for you."
        actions={
          unread ? (
            <Button
              variant="secondary"
              size="sm"
              leftIcon={<CheckCheck className="h-4 w-4" />}
              onClick={() => readAll.mutate()}
              loading={readAll.isPending}
            >
              Mark all read
            </Button>
          ) : undefined
        }
      />
      {isLoading ? (
        <SkeletonRows rows={4} />
      ) : isError ? (
        <InlineError message="Couldn't load updates." onRetry={refetch} />
      ) : !data?.length ? (
        <EmptyState icon={<Bell className="h-6 w-6" />} title="No updates yet" description="Updates from your tasks and routines will appear here." />
      ) : (
        <div className="space-y-3">
          {data.map((u) => (
            <UpdateCard key={u.id} update={u} onRead={read.mutate} />
          ))}
        </div>
      )}
    </div>
  );
};

export default Updates;
