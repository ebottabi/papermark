import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BookOpen, Pencil, Plus, Trash2 } from "lucide-react";
import { knowledgeApi } from "@/api/library";
import { ApiError } from "@/lib/http";
import type { Knowledge } from "@/types";
import {
  Button,
  ConfirmDialog,
  Dialog,
  EmptyState,
  Field,
  InlineError,
  Input,
  RowMenu,
  SkeletonRows,
  Switch,
  Textarea,
  PageHeader,
  toast,
} from "@/components/ui";

const KnowledgePage = () => {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<Knowledge | "new" | null>(null);
  const [form, setForm] = useState({ title: "", content: "", enabled: true });
  const [removing, setRemoving] = useState<Knowledge | null>(null);

  const { data, isLoading, isError, refetch } = useQuery({ queryKey: ["knowledge"], queryFn: knowledgeApi.list });

  const invalidate = () => qc.invalidateQueries({ queryKey: ["knowledge"] });
  const onError = (e: unknown) => toast.error("Something went wrong", e instanceof ApiError ? e.message : undefined);

  const save = useMutation({
    mutationFn: () =>
      editing === "new" ? knowledgeApi.create(form) : knowledgeApi.patch((editing as Knowledge).id, form),
    onSuccess: () => {
      invalidate();
      setEditing(null);
    },
    onError,
  });
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => knowledgeApi.patch(id, { enabled }),
    onSuccess: invalidate,
    onError,
  });
  const remove = useMutation({
    mutationFn: (id: string) => knowledgeApi.remove(id),
    onSuccess: () => {
      invalidate();
      setRemoving(null);
    },
    onError,
  });

  const open = (k: Knowledge | "new") => {
    setEditing(k);
    setForm(k === "new" ? { title: "", content: "", enabled: true } : { title: k.title, content: k.content, enabled: k.enabled });
  };

  return (
    <div>
      <PageHeader
        title="Knowledge"
        description="Reference material Keen can draw on. Toggle entries on or off."
        actions={
          <Button size="sm" leftIcon={<Plus className="h-4 w-4" />} onClick={() => open("new")}>
            Add entry
          </Button>
        }
      />

      {isLoading ? (
        <SkeletonRows rows={3} />
      ) : isError ? (
        <InlineError message="Couldn't load knowledge." onRetry={refetch} />
      ) : !data?.length ? (
        <EmptyState
          icon={<BookOpen className="h-6 w-6" />}
          title="No knowledge yet"
          description="Add reference notes, docs, or facts Keen should use."
          action={<Button onClick={() => open("new")}>Add entry</Button>}
        />
      ) : (
        <ul className="space-y-2">
          {data.map((k) => (
            <li key={k.id} className="flex items-start justify-between gap-3 rounded-xl border border-gray-200 bg-background-surface p-4">
              <div className="min-w-0">
                <h3 className="text-body-1 font-medium text-ink">{k.title}</h3>
                <p className="mt-1 line-clamp-2 text-body-2 text-ink-muted">{k.content}</p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Switch
                  checked={k.enabled}
                  onCheckedChange={(enabled) => toggle.mutate({ id: k.id, enabled })}
                  aria-label={k.enabled ? "Disable entry" : "Enable entry"}
                />
                <RowMenu
                  actions={[
                    { label: "Edit", icon: <Pencil className="h-4 w-4" />, onSelect: () => open(k) },
                    { label: "Delete", icon: <Trash2 className="h-4 w-4" />, destructive: true, onSelect: () => setRemoving(k) },
                  ]}
                />
              </div>
            </li>
          ))}
        </ul>
      )}

      <Dialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing === "new" ? "Add knowledge" : "Edit knowledge"}
        footer={
          <>
            <Button variant="secondary" onClick={() => setEditing(null)}>
              Cancel
            </Button>
            <Button onClick={() => save.mutate()} loading={save.isPending} disabled={!form.title.trim()}>
              Save
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="Title" required>
            <Input autoFocus value={form.title} onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))} />
          </Field>
          <Field label="Content">
            <Textarea rows={6} value={form.content} onChange={(e) => setForm((f) => ({ ...f, content: e.target.value }))} />
          </Field>
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title="Delete entry?"
        confirmLabel="Delete"
        destructive
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </div>
  );
};

export default KnowledgePage;
