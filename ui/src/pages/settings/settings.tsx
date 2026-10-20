import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@/hooks/use-auth";
import { preferencesApi, usageApi } from "@/api/library";
import { formatNumber } from "@/lib/utils";
import { ApiError } from "@/lib/http";
import type { Preferences } from "@/types";
import {
  Avatar,
  Button,
  Card,
  Field,
  Input,
  Loader,
  StatCard,
  Tabs,
  PageHeader,
  toast,
} from "@/components/ui";

const PreferencesForm = () => {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["preferences"], queryFn: preferencesApi.get });
  const [form, setForm] = useState<Preferences | null>(null);

  useEffect(() => {
    if (data && !form) setForm(data);
  }, [data, form]);

  const save = useMutation({
    mutationFn: (body: Partial<Preferences>) => preferencesApi.patch(body),
    onSuccess: (p) => {
      qc.setQueryData(["preferences"], p);
      toast.success("Preferences saved");
    },
    onError: (e) => toast.error("Couldn't save", e instanceof ApiError ? e.message : undefined),
  });

  if (!form) return <Loader label="Loading preferences…" />;

  const set = (patch: Partial<Preferences>) => setForm((f) => ({ ...f!, ...patch }));

  return (
    <div className="max-w-xl space-y-5">
      <div className="grid grid-cols-2 gap-4">
        <Field label="Timezone">
          <Input value={form.timezone} onChange={(e) => set({ timezone: e.target.value })} placeholder="America/New_York" />
        </Field>
        <Field label="Daily push limit" hint="Max push notifications per day.">
          <Input type="number" value={form.pushLimit} onChange={(e) => set({ pushLimit: Number(e.target.value) })} />
        </Field>
        <Field label="Quiet hours start">
          <Input type="time" value={form.quietStart} onChange={(e) => set({ quietStart: e.target.value })} />
        </Field>
        <Field label="Quiet hours end">
          <Input type="time" value={form.quietEnd} onChange={(e) => set({ quietEnd: e.target.value })} />
        </Field>
      </div>
      <Field label="News topics" hint="Comma-separated interests for your briefings.">
        <Input
          value={form.newsTopics.join(", ")}
          onChange={(e) => set({ newsTopics: e.target.value.split(",").map((t) => t.trim()).filter(Boolean) })}
          placeholder="ai, markets, climate"
        />
      </Field>
      <Field label="ntfy topic" hint="Optional topic for push delivery via ntfy.">
        <Input value={form.ntfyTopic ?? ""} onChange={(e) => set({ ntfyTopic: e.target.value || null })} />
      </Field>
      <Button onClick={() => save.mutate(form)} loading={save.isPending}>
        Save preferences
      </Button>
    </div>
  );
};

const UsagePanel = () => {
  const { data, isLoading } = useQuery({ queryKey: ["usage"], queryFn: () => usageApi.get(30) });
  if (isLoading || !data) return <Loader label="Loading usage…" />;
  const t = data.total;
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Input tokens" value={formatNumber(t.inputTokens)} sub="Last 30 days" />
        <StatCard label="Output tokens" value={formatNumber(t.outputTokens)} sub="Last 30 days" />
        <StatCard label="Cache read" value={formatNumber(t.cacheReadTokens)} />
        <StatCard label="Cache write" value={formatNumber(t.cacheWriteTokens)} />
      </div>
      <Card className="overflow-hidden p-0">
        <table className="w-full text-body-3">
          <thead className="bg-gray-50 text-ink-muted">
            <tr>
              <th className="px-4 py-2 text-left font-medium">Date</th>
              <th className="px-4 py-2 text-right font-medium">Tasks</th>
              <th className="px-4 py-2 text-right font-medium">Input</th>
              <th className="px-4 py-2 text-right font-medium">Output</th>
            </tr>
          </thead>
          <tbody>
            {data.days.map((day) => (
              <tr key={day.date} className="border-t border-gray-200">
                <td className="px-4 py-2 text-ink">{day.date}</td>
                <td className="px-4 py-2 text-right tabular-nums text-ink-muted">{day.tasks}</td>
                <td className="px-4 py-2 text-right tabular-nums text-ink-muted">{formatNumber(day.usage.inputTokens)}</td>
                <td className="px-4 py-2 text-right tabular-nums text-ink-muted">{formatNumber(day.usage.outputTokens)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </div>
  );
};

const Settings = () => {
  const { user, logout } = useAuth();
  const [tab, setTab] = useState("preferences");

  return (
    <div>
      <PageHeader title="Settings" description="Your account, preferences, and usage." />

      <Card className="mb-6 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Avatar name={user?.name ?? user?.email ?? "You"} src={user?.picture ?? undefined} />
          <div>
            <p className="text-body-1 font-medium text-ink">{user?.name ?? "You"}</p>
            <p className="text-body-3 text-ink-faint">{user?.email}</p>
          </div>
        </div>
        <Button variant="secondary" size="sm" onClick={logout}>
          Sign out
        </Button>
      </Card>

      <Tabs
        className="mb-6"
        value={tab}
        onValueChange={setTab}
        items={[
          { value: "preferences", label: "Preferences" },
          { value: "usage", label: "Usage" },
        ]}
      />

      {tab === "preferences" ? <PreferencesForm /> : <UsagePanel />}
    </div>
  );
};

export default Settings;
