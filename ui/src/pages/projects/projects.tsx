import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderKanban, Pencil, Plus, Trash2 } from "lucide-react";
import { projectsApi } from "@/api/library";
import { ApiError } from "@/lib/http";
import type { Project } from "@/types";
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

const Projects = () => {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<Project | "new" | null>(null);
  const [form, setForm] = useState({ name: "", instructions: "" });
  const [removing, setRemoving] = useState<Project | null>(null);

  const { data, isLoading, isError, refetch } = useQuery({ queryKey: ["projects"], queryFn: projectsApi.list });

  const invalidate = () => qc.invalidateQueries({ queryKey: ["projects"] });
  const onError = (e: unknown) => toast.error("Something went wrong", e instanceof ApiError ? e.message : undefined);

  const save = useMutation({
    mutationFn: () =>
      editing === "new"
        ? projectsApi.create(form)
        : projectsApi.patch((editing as Project).id, form),
    onSuccess: () => {
      invalidate();
      setEditing(null);
    },
    onError,
  });
  const remove = useMutation({
    mutationFn: (id: string) => projectsApi.remove(id),
    onSuccess: () => {
      invalidate();
      setRemoving(null);
    },
    onError,
  });

  const open = (p: Project | "new") => {
    setEditing(p);
    setForm(p === "new" ? { name: "", instructions: "" } : { name: p.name, instructions: p.instructions });
  };

  return (
    <div>
      <PageHeader
        title="Projects"
        description="Reusable context and instructions Keen applies to grouped tasks."
        actions={
          <Button size="sm" leftIcon={<Plus className="h-4 w-4" />} onClick={() => open("new")}>
            New project
          </Button>
        }
      />

      {isLoading ? (
        <SkeletonRows rows={3} />
      ) : isError ? (
        <InlineError message="Couldn't load projects." onRetry={refetch} />
      ) : !data?.length ? (
        <EmptyState
          icon={<FolderKanban className="h-6 w-6" />}
          title="No projects yet"
          description="Group related tasks under shared instructions."
          action={<Button onClick={() => open("new")}>New project</Button>}
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {data.map((p) => (
            <Card key={p.id} className="flex flex-col">
              <div className="flex items-start justify-between gap-2">
                <h3 className="text-body-1 font-medium text-ink">{p.name}</h3>
                <RowMenu
                  actions={[
                    { label: "Edit", icon: <Pencil className="h-4 w-4" />, onSelect: () => open(p) },
                    { label: "Delete", icon: <Trash2 className="h-4 w-4" />, destructive: true, onSelect: () => setRemoving(p) },
                  ]}
                />
              </div>
              <p className="mt-2 line-clamp-4 text-body-2 text-ink-muted">{p.instructions || "No instructions."}</p>
            </Card>
          ))}
        </div>
      )}

      <Dialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing === "new" ? "New project" : "Edit project"}
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
          <Field label="Name" required>
            <Input autoFocus value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="Q4 launch" />
          </Field>
          <Field label="Instructions" hint="Context Keen should apply to tasks in this project.">
            <Textarea rows={5} value={form.instructions} onChange={(e) => setForm((f) => ({ ...f, instructions: e.target.value }))} />
          </Field>
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title="Delete project?"
        description="Tasks keep running, but lose this shared context."
        confirmLabel="Delete"
        destructive
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </div>
  );
};

export default Projects;
