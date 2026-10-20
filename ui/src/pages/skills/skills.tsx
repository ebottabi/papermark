import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, Sparkles, Trash2 } from "lucide-react";
import { skillsApi } from "@/api/library";
import { ApiError } from "@/lib/http";
import type { Skill } from "@/types";
import {
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  Field,
  InlineError,
  Input,
  RowMenu,
  SkeletonRows,
  Textarea,
  PageHeader,
  toast,
} from "@/components/ui";

const Skills = () => {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<Skill | "new" | null>(null);
  const [form, setForm] = useState({ name: "", description: "", instructions: "" });
  const [removing, setRemoving] = useState<Skill | null>(null);

  const { data, isLoading, isError, refetch } = useQuery({ queryKey: ["skills"], queryFn: skillsApi.list });

  const invalidate = () => qc.invalidateQueries({ queryKey: ["skills"] });
  const onError = (e: unknown) => toast.error("Something went wrong", e instanceof ApiError ? e.message : undefined);

  const save = useMutation({
    mutationFn: () => (editing === "new" ? skillsApi.create(form) : skillsApi.patch((editing as Skill).id, form)),
    onSuccess: () => {
      invalidate();
      setEditing(null);
    },
    onError,
  });
  const remove = useMutation({
    mutationFn: (id: string) => skillsApi.remove(id),
    onSuccess: () => {
      invalidate();
      setRemoving(null);
    },
    onError,
  });

  const open = (s: Skill | "new") => {
    setEditing(s);
    setForm(s === "new" ? { name: "", description: "", instructions: "" } : { name: s.name, description: s.description, instructions: s.instructions });
  };

  return (
    <div>
      <PageHeader
        title="Skills"
        description="Custom procedures Keen can invoke by name."
        actions={
          <Button size="sm" leftIcon={<Plus className="h-4 w-4" />} onClick={() => open("new")}>
            New skill
          </Button>
        }
      />

      {isLoading ? (
        <SkeletonRows rows={3} />
      ) : isError ? (
        <InlineError message="Couldn't load skills." onRetry={refetch} />
      ) : !data?.length ? (
        <EmptyState
          icon={<Sparkles className="h-6 w-6" />}
          title="No skills yet"
          description="Teach Keen a repeatable procedure it can reuse."
          action={<Button onClick={() => open("new")}>New skill</Button>}
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {data.map((s) => (
            <Card key={s.id}>
              <div className="flex items-start justify-between gap-2">
                <h3 className="font-mono text-body-1 font-medium text-ink">{s.name}</h3>
                <RowMenu
                  actions={[
                    { label: "Edit", icon: <Pencil className="h-4 w-4" />, onSelect: () => open(s) },
                    { label: "Delete", icon: <Trash2 className="h-4 w-4" />, destructive: true, onSelect: () => setRemoving(s) },
                  ]}
                />
              </div>
              <p className="mt-2 line-clamp-3 text-body-2 text-ink-muted">{s.description || "No description."}</p>
            </Card>
          ))}
        </div>
      )}

      <Dialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing === "new" ? "New skill" : "Edit skill"}
        footer={
          <>
            <Button variant="secondary" onClick={() => setEditing(null)}>
              Cancel
            </Button>
            <Button onClick={() => save.mutate()} loading={save.isPending} disabled={!form.name.trim()}>
              Save
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="Name" required hint="A short identifier, e.g. weekly-digest.">
            <Input autoFocus value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} className="font-mono" />
          </Field>
          <Field label="Description" hint="When should Keen use this skill?">
            <Input value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
          </Field>
          <Field label="Instructions">
            <Textarea rows={6} value={form.instructions} onChange={(e) => setForm((f) => ({ ...f, instructions: e.target.value }))} />
          </Field>
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title="Delete skill?"
        confirmLabel="Delete"
        destructive
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </div>
  );
};

export default Skills;
