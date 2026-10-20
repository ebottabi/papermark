import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarClock, Clock, Eye, Mail, Play, Plus, Trash2 } from "lucide-react";
import { schedulesApi, type NewSchedule } from "@/api/schedules";
import { timeAgo } from "@/lib/utils";
import { ApiError } from "@/lib/http";
import type { EmailRule, ScheduledTask, Trigger, WatchRule } from "@/types";
import {
  Button,
  ConfirmDialog,
  Dialog,
  EmptyState,
  Field,
  InlineError,
  Input,
  RowMenu,
  Select,
  SkeletonRows,
  Switch,
  Tabs,
  Textarea,
  PageHeader,
  toast,
} from "@/components/ui";

const TRIGGER_ICON: Record<Trigger, React.ReactNode> = {
  time: <Clock className="h-5 w-5" />,
  email: <Mail className="h-5 w-5" />,
  watch: <Eye className="h-5 w-5" />,
};

const summarize = (s: ScheduledTask): string => {
  if (s.trigger === "time") return `${s.repeat ?? "once"}${s.time ? ` at ${s.time}` : ""}`;
  if (s.trigger === "email") {
    const r = (s.rule ?? {}) as EmailRule;
    return r.smart || [r.from && `from ${r.from}`, r.subject && `subject "${r.subject}"`].filter(Boolean).join(", ") || "any new email";
  }
  const r = (s.rule ?? {}) as WatchRule;
  return `${r.kind === "search" ? r.query : r.url} · every ${r.intervalMinutes}m`;
};

type Draft = {
  title: string;
  prompt: string;
  mode: ScheduledTask["mode"];
  trigger: Trigger;
  repeat: NonNullable<ScheduledTask["repeat"]>;
  time: string;
  email: EmailRule;
  watch: WatchRule;
};

const emptyDraft = (): Draft => ({
  title: "",
  prompt: "",
  mode: "quality",
  trigger: "time",
  repeat: "daily",
  time: "09:00",
  email: { from: "", subject: "", smart: "" },
  watch: { kind: "page", url: "", condition: "changed", intervalMinutes: 60 },
});

const toBody = (d: Draft): NewSchedule => {
  const base: NewSchedule = { title: d.title, prompt: d.prompt, mode: d.mode, kind: "custom", trigger: d.trigger };
  if (d.trigger === "time") return { ...base, repeat: d.repeat, time: d.time };
  if (d.trigger === "email") {
    const rule: EmailRule = Object.fromEntries(Object.entries(d.email).filter(([, v]) => v));
    return { ...base, rule };
  }
  return { ...base, rule: d.watch };
};

const Schedules = () => {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [d, setD] = useState<Draft>(emptyDraft);
  const [removing, setRemoving] = useState<ScheduledTask | null>(null);

  const { data, isLoading, isError, refetch } = useQuery({ queryKey: ["schedules"], queryFn: schedulesApi.list });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["schedules"] });
  const onError = (e: unknown) => toast.error("Something went wrong", e instanceof ApiError ? e.message : undefined);

  const create = useMutation({
    mutationFn: () => schedulesApi.create(toBody(d)),
    onSuccess: () => {
      invalidate();
      setOpen(false);
      setD(emptyDraft());
    },
    onError,
  });
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => schedulesApi.patch(id, { enabled }),
    onSuccess: invalidate,
    onError,
  });
  const run = useMutation({
    mutationFn: (id: string) => schedulesApi.run(id),
    onSuccess: () => toast.success("Routine triggered"),
    onError,
  });
  const remove = useMutation({
    mutationFn: (id: string) => schedulesApi.remove(id),
    onSuccess: () => {
      invalidate();
      setRemoving(null);
    },
    onError,
  });

  const set = (patch: Partial<Draft>) => setD((prev) => ({ ...prev, ...patch }));

  return (
    <div>
      <PageHeader
        title="Routines"
        description="Tasks Keen runs on a schedule, on new email, or when something changes."
        actions={
          <Button size="sm" leftIcon={<Plus className="h-4 w-4" />} onClick={() => setOpen(true)}>
            New routine
          </Button>
        }
      />

      {isLoading ? (
        <SkeletonRows rows={3} />
      ) : isError ? (
        <InlineError message="Couldn't load routines." onRetry={refetch} />
      ) : !data?.length ? (
        <EmptyState
          icon={<CalendarClock className="h-6 w-6" />}
          title="No routines yet"
          description="Automate a recurring task or react to a trigger."
          action={<Button onClick={() => setOpen(true)}>New routine</Button>}
        />
      ) : (
        <div className="space-y-3">
          {data.map((s) => (
            <div key={s.id} className="flex items-start justify-between gap-3 rounded-xl border border-gray-200 bg-background-surface p-4">
              <div className="flex min-w-0 items-start gap-3">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-ink-muted">
                  {TRIGGER_ICON[s.trigger]}
                </div>
                <div className="min-w-0">
                  <h3 className="truncate text-body-1 font-medium text-ink">{s.title || s.prompt}</h3>
                  <p className="truncate text-body-3 text-ink-muted">{summarize(s)}</p>
                  <p className="mt-0.5 text-body-4 text-ink-faint">
                    {s.nextRunAt ? `Next ${timeAgo(s.nextRunAt)}` : s.lastRunAt ? `Last ran ${timeAgo(s.lastRunAt)}` : "Not yet run"}
                  </p>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Switch checked={s.enabled} onCheckedChange={(enabled) => toggle.mutate({ id: s.id, enabled })} aria-label="Toggle routine" />
                <RowMenu
                  actions={[
                    { label: "Run now", icon: <Play className="h-4 w-4" />, onSelect: () => run.mutate(s.id) },
                    { label: "Delete", icon: <Trash2 className="h-4 w-4" />, destructive: true, onSelect: () => setRemoving(s) },
                  ]}
                />
              </div>
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title="New routine"
        footer={
          <>
            <Button variant="secondary" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button onClick={() => create.mutate()} loading={create.isPending} disabled={!d.prompt.trim()}>
              Create
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="Title">
            <Input value={d.title} onChange={(e) => set({ title: e.target.value })} placeholder="Morning briefing" />
          </Field>
          <Field label="What should Keen do?" required>
            <Textarea rows={3} value={d.prompt} onChange={(e) => set({ prompt: e.target.value })} placeholder="Summarize overnight email and news." />
          </Field>

          <Field label="Trigger">
            <Tabs
              value={d.trigger}
              onValueChange={(v) => set({ trigger: v as Trigger })}
              items={[
                { value: "time", label: "Schedule" },
                { value: "email", label: "On email" },
                { value: "watch", label: "On change" },
              ]}
            />
          </Field>

          {d.trigger === "time" ? (
            <div className="grid grid-cols-2 gap-3">
              <Field label="Repeat">
                <Select
                  value={d.repeat}
                  onValueChange={(v) => set({ repeat: v as Draft["repeat"] })}
                  options={[
                    { value: "hourly", label: "Hourly" },
                    { value: "daily", label: "Daily" },
                    { value: "weekdays", label: "Weekdays" },
                    { value: "weekly", label: "Weekly" },
                  ]}
                />
              </Field>
              {d.repeat !== "hourly" ? (
                <Field label="Time">
                  <Input type="time" value={d.time} onChange={(e) => set({ time: e.target.value })} />
                </Field>
              ) : null}
            </div>
          ) : d.trigger === "email" ? (
            <div className="space-y-4">
              <Field label="From" hint="Sender address to match (optional).">
                <Input value={d.email.from} onChange={(e) => set({ email: { ...d.email, from: e.target.value } })} placeholder="boss@company.com" />
              </Field>
              <Field label="Subject contains">
                <Input value={d.email.subject} onChange={(e) => set({ email: { ...d.email, subject: e.target.value } })} />
              </Field>
              <Field label="Smart filter" hint="Describe which emails should trigger this.">
                <Input value={d.email.smart} onChange={(e) => set({ email: { ...d.email, smart: e.target.value } })} placeholder="anything that needs a reply today" />
              </Field>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <Field label="Watch">
                  <Select
                    value={d.watch.kind}
                    onValueChange={(v) => set({ watch: { ...d.watch, kind: v as WatchRule["kind"] } })}
                    options={[
                      { value: "page", label: "A web page" },
                      { value: "search", label: "A search" },
                    ]}
                  />
                </Field>
                <Field label="Check every">
                  <Select
                    value={String(d.watch.intervalMinutes)}
                    onValueChange={(v) => set({ watch: { ...d.watch, intervalMinutes: Number(v) } })}
                    options={[
                      { value: "15", label: "15 minutes" },
                      { value: "60", label: "1 hour" },
                      { value: "360", label: "6 hours" },
                      { value: "1440", label: "1 day" },
                    ]}
                  />
                </Field>
              </div>
              <Field label={d.watch.kind === "search" ? "Query" : "URL"} required>
                {d.watch.kind === "search" ? (
                  <Input value={d.watch.query ?? ""} onChange={(e) => set({ watch: { ...d.watch, query: e.target.value } })} />
                ) : (
                  <Input value={d.watch.url ?? ""} onChange={(e) => set({ watch: { ...d.watch, url: e.target.value } })} placeholder="https://…" />
                )}
              </Field>
              <Field label="Notify when">
                <Select
                  value={d.watch.condition}
                  onValueChange={(v) => set({ watch: { ...d.watch, condition: v as WatchRule["condition"] } })}
                  options={[
                    { value: "changed", label: "It changes" },
                    { value: "newItems", label: "There are new items" },
                    { value: "contains", label: "It contains text" },
                    { value: "below", label: "A value drops below" },
                    { value: "above", label: "A value rises above" },
                  ]}
                />
              </Field>
              {d.watch.condition === "contains" ? (
                <Field label="Text">
                  <Input value={d.watch.text ?? ""} onChange={(e) => set({ watch: { ...d.watch, text: e.target.value } })} />
                </Field>
              ) : d.watch.condition === "below" || d.watch.condition === "above" ? (
                <Field label="Threshold">
                  <Input
                    type="number"
                    value={d.watch.threshold ?? ""}
                    onChange={(e) => set({ watch: { ...d.watch, threshold: Number(e.target.value) } })}
                  />
                </Field>
              ) : null}
            </div>
          )}
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title="Delete routine?"
        confirmLabel="Delete"
        destructive
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </div>
  );
};

export default Schedules;
