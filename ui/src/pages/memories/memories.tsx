import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Brain, Pencil, Plus, Trash2 } from "lucide-react";
import { memoriesApi } from "@/api/library";
import { timeAgo } from "@/lib/utils";
import { ApiError } from "@/lib/http";
import type { Memory } from "@/types";
import {
  Button,
  ConfirmDialog,
  Dialog,
  EmptyState,
  Field,
  InlineError,
  RowMenu,
  SkeletonRows,
  Textarea,
  PageHeader,
  toast,
} from "@/components/ui";

const Memories = () => {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<Memory | "new" | null>(null);
  const [text, setText] = useState("");
  const [removing, setRemoving] = useState<Memory | null>(null);

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ["memories"],
    queryFn: memoriesApi.list,
  });

  const invalidate = () => qc.invalidateQueries({ queryKey: ["memories"] });
  const onError = (e: unknown) =>
    toast.error("Something went wrong", e instanceof ApiError ? e.message : undefined);

  const save = useMutation({
    mutationFn: () =>
      editing === "new" ? memoriesApi.create(text.trim()) : memoriesApi.patch((editing as Memory).id, text.trim()),
    onSuccess: () => {
      invalidate();
      setEditing(null);
    },
    onError,
  });
  const remove = useMutation({
    mutationFn: (id: string) => memoriesApi.remove(id),
    onSuccess: () => {
      invalidate();
      setRemoving(null);
    },
    onError,
  });

  const open = (m: Memory | "new") => {
    setEditing(m);
    setText(m === "new" ? "" : m.text);
  };

  return (
    <div>
      <PageHeader
        title="Memories"
        description="Facts Keen remembers about you across tasks."
        actions={
          <Button size="sm" leftIcon={<Plus className="h-4 w-4" />} onClick={() => open("new")}>
            Add memory
          </Button>
        }
      />

      {isLoading ? (
        <SkeletonRows rows={4} />
      ) : isError ? (
        <InlineError message="Couldn't load memories." onRetry={refetch} />
      ) : !data?.length ? (
        <EmptyState
          icon={<Brain className="h-6 w-6" />}
          title="No memories yet"
          description="Add something you want Keen to always keep in mind."
          action={<Button onClick={() => open("new")}>Add memory</Button>}
        />
      ) : (
        <ul className="space-y-2">
          {data.map((m) => (
            <li
              key={m.id}
              className="flex items-start justify-between gap-3 rounded-xl border border-gray-200 bg-background-surface p-4"
            >
              <div className="min-w-0">
                <p className="whitespace-pre-wrap text-body-2 text-ink">{m.text}</p>
                <p className="mt-1.5 text-body-4 text-ink-faint">Updated {timeAgo(m.updatedAt)}</p>
              </div>
              <RowMenu
                actions={[
                  { label: "Edit", icon: <Pencil className="h-4 w-4" />, onSelect: () => open(m) },
                  { label: "Delete", icon: <Trash2 className="h-4 w-4" />, destructive: true, onSelect: () => setRemoving(m) },
                ]}
              />
            </li>
          ))}
        </ul>
      )}

      <Dialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing === "new" ? "Add memory" : "Edit memory"}
        footer={
          <>
            <Button variant="secondary" onClick={() => setEditing(null)}>
              Cancel
            </Button>
            <Button onClick={() => save.mutate()} loading={save.isPending} disabled={!text.trim()}>
              Save
            </Button>
          </>
        }
      >
        <Field label="Memory">
          <Textarea rows={4} autoFocus value={text} onChange={(e) => setText(e.target.value)} placeholder="e.g. I prefer concise summaries." />
        </Field>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title="Delete memory?"
        description="Keen will forget this. This can't be undone."
        confirmLabel="Delete"
        destructive
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </div>
  );
};

export default Memories;
