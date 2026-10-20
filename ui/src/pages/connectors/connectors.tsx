import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Calendar, Contact, HardDrive, Mail, Plug, Plus, Star, Trash2 } from "lucide-react";
import { connectorsApi } from "@/api/connectors";
import { ApiError } from "@/lib/http";
import type { Connector } from "@/types";
import {
  Button,
  ConfirmDialog,
  ConnectorStatusBadge,
  Dialog,
  EmptyState,
  Field,
  InlineError,
  Input,
  RowMenu,
  SkeletonRows,
  PageHeader,
  toast,
} from "@/components/ui";

const KIND_ICON: Record<Connector["kind"], React.ReactNode> = {
  gmail: <Mail className="h-5 w-5" />,
  calendar: <Calendar className="h-5 w-5" />,
  contacts: <Contact className="h-5 w-5" />,
  drive: <HardDrive className="h-5 w-5" />,
  mcp: <Plug className="h-5 w-5" />,
};

const openAuth = (url: string) => window.open(url, "_blank", "noopener");

const Connectors = () => {
  const qc = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState({ name: "", url: "" });
  const [removing, setRemoving] = useState<Connector | null>(null);

  const { data, isLoading, isError, refetch } = useQuery({ queryKey: ["connectors"], queryFn: connectorsApi.list });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["connectors"] });
  const onError = (e: unknown) => toast.error("Something went wrong", e instanceof ApiError ? e.message : undefined);

  const add = useMutation({
    mutationFn: () => connectorsApi.add(form),
    onSuccess: () => {
      invalidate();
      setAdding(false);
      setForm({ name: "", url: "" });
    },
    onError,
  });
  const remove = useMutation({
    mutationFn: (name: string) => connectorsApi.remove(name),
    onSuccess: () => {
      invalidate();
      setRemoving(null);
    },
    onError,
  });
  const authorize = useMutation({
    mutationFn: (name: string) => connectorsApi.authorize(name),
    onSuccess: ({ authorizationUrl }) => openAuth(authorizationUrl),
    onError,
  });
  const reconnect = useMutation({
    mutationFn: ({ kind, id }: { kind: string; id: string }) => connectorsApi.reconnectAccount(kind, id),
    onSuccess: ({ authorizationUrl }) => openAuth(authorizationUrl),
    onError,
  });
  const makePrimary = useMutation({
    mutationFn: ({ kind, id }: { kind: string; id: string }) => connectorsApi.makePrimary(kind, id),
    onSuccess: invalidate,
    onError,
  });
  const removeAccount = useMutation({
    mutationFn: ({ kind, id }: { kind: string; id: string }) => connectorsApi.removeAccount(kind, id),
    onSuccess: invalidate,
    onError,
  });

  return (
    <div>
      <PageHeader
        title="Connectors"
        description="Services Keen can act through — email, calendar, and MCP tools."
        actions={
          <Button size="sm" leftIcon={<Plus className="h-4 w-4" />} onClick={() => setAdding(true)}>
            Add MCP server
          </Button>
        }
      />

      {isLoading ? (
        <SkeletonRows rows={3} />
      ) : isError ? (
        <InlineError message="Couldn't load connectors." onRetry={refetch} />
      ) : !data?.length ? (
        <EmptyState
          icon={<Plug className="h-6 w-6" />}
          title="No connectors yet"
          description="Connect a service or add an MCP server to give Keen more tools."
          action={<Button onClick={() => setAdding(true)}>Add MCP server</Button>}
        />
      ) : (
        <div className="space-y-3">
          {data.map((c) => (
            <div key={c.name} className="rounded-xl border border-gray-200 bg-background-surface p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-start gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-ink-muted">
                    {KIND_ICON[c.kind]}
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="truncate text-body-1 font-medium text-ink">{c.name}</h3>
                      <ConnectorStatusBadge status={c.status} />
                    </div>
                    <p className="truncate text-body-3 text-ink-faint">
                      {c.kind === "mcp" ? c.url : `${c.tools} tool${c.tools === 1 ? "" : "s"}`}
                      {c.error ? ` · ${c.error}` : ""}
                    </p>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {c.status === "needs_auth" ? (
                    <Button size="sm" variant="secondary" onClick={() => authorize.mutate(c.name)} loading={authorize.isPending}>
                      Connect
                    </Button>
                  ) : null}
                  <RowMenu
                    actions={[
                      { label: "Re-authorize", onSelect: () => authorize.mutate(c.name) },
                      { label: "Remove", icon: <Trash2 className="h-4 w-4" />, destructive: true, onSelect: () => setRemoving(c) },
                    ]}
                  />
                </div>
              </div>

              {c.accounts.length ? (
                <ul className="mt-3 space-y-1.5 border-t border-gray-200/70 pt-3">
                  {c.accounts.map((a) => (
                    <li key={a.id} className="flex items-center justify-between gap-2 text-body-3">
                      <span className="flex min-w-0 items-center gap-2">
                        <span className="truncate text-ink">{a.email}</span>
                        {a.primary ? <Star className="h-3.5 w-3.5 fill-high text-high" /> : null}
                        <ConnectorStatusBadge status={a.status} />
                      </span>
                      <RowMenu
                        actions={[
                          ...(a.primary ? [] : [{ label: "Make primary", onSelect: () => makePrimary.mutate({ kind: c.kind, id: a.id }) }]),
                          { label: "Reconnect", onSelect: () => reconnect.mutate({ kind: c.kind, id: a.id }) },
                          { label: "Remove", icon: <Trash2 className="h-4 w-4" />, destructive: true, onSelect: () => removeAccount.mutate({ kind: c.kind, id: a.id }) },
                        ]}
                      />
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={adding}
        onOpenChange={setAdding}
        title="Add MCP server"
        description="Connect a Model Context Protocol server to extend Keen's tools."
        footer={
          <>
            <Button variant="secondary" onClick={() => setAdding(false)}>
              Cancel
            </Button>
            <Button onClick={() => add.mutate()} loading={add.isPending} disabled={!form.name.trim() || !form.url.trim()}>
              Add
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="Name" required>
            <Input autoFocus value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="my-tools" />
          </Field>
          <Field label="Server URL" required>
            <Input value={form.url} onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))} placeholder="https://example.com/mcp" />
          </Field>
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.name ?? "connector"}?`}
        description="Keen loses access to these tools until reconnected."
        confirmLabel="Remove"
        destructive
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.name)}
      />
    </div>
  );
};

export default Connectors;
