import { useEffect, useState } from "react";
import { Download, Loader2 } from "lucide-react";
import { Button, Dialog, toast } from "@/components/ui";
import { ApiError } from "@/lib/http";
import { Markdown } from "./markdown";
import { downloadTaskFile, fetchTaskFileBlob } from "./download";

type Kind = "markdown" | "image" | "text" | "binary";

// Pick a preview by extension; the files endpoint doesn't promise a content type.
function classify(path: string): Kind {
  if (/\.(md|markdown)$/i.test(path)) return "markdown";
  if (/\.(png|jpe?g|gif|svg|webp|avif)$/i.test(path)) return "image";
  if (
    /\.(txt|json|jsonl|csv|tsv|log|ya?ml|xml|html?|css|s[ac]ss|jsx?|tsx?|py|go|rs|rb|java|kt|c|h|cpp|cc|sh|bash|zsh|sql|toml|ini|env|conf|mjs|cjs)$/i.test(
      path,
    )
  )
    return "text";
  return "binary";
}

export const FileViewer = ({ taskId, path, onClose }: { taskId: string; path: string; onClose: () => void }) => {
  const kind = classify(path);
  const name = path.split("/").pop() || path;
  const [text, setText] = useState<string | null>(null);
  const [imgUrl, setImgUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(kind !== "binary");
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (kind === "binary") return;
    let active = true;
    let url: string | null = null;
    setLoading(true);
    setFailed(false);
    fetchTaskFileBlob(taskId, path)
      .then(async (blob) => {
        if (!active) return;
        if (kind === "image") {
          url = URL.createObjectURL(blob);
          setImgUrl(url);
        } else {
          setText(await blob.text());
        }
      })
      .catch(() => active && setFailed(true))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [taskId, path, kind]);

  const download = async () => {
    try {
      await downloadTaskFile(taskId, path);
    } catch (e) {
      toast.error("Couldn't download file", e instanceof ApiError ? e.message : undefined);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={name}
      className="max-w-3xl"
      footer={
        <Button variant="secondary" onClick={download}>
          <Download className="h-4 w-4" />
          Download
        </Button>
      }
    >
      <div className="scrollbar-thin max-h-[70vh] overflow-auto">
        {loading ? (
          <div className="flex items-center gap-2 py-8 text-body-3 text-ink-faint">
            <Loader2 className="h-4 w-4 animate-spin" />
            Loading…
          </div>
        ) : failed ? (
          <div className="py-8 text-body-3 text-crit">Couldn't load this file. Try downloading it instead.</div>
        ) : kind === "markdown" && text != null ? (
          <Markdown>{text}</Markdown>
        ) : kind === "image" && imgUrl ? (
          <img src={imgUrl} alt={name} className="mx-auto max-h-[68vh] rounded-md" />
        ) : kind === "text" && text != null ? (
          <pre className="scrollbar-thin overflow-auto whitespace-pre-wrap rounded-md border border-gray-200 bg-gray-50 p-3 font-mono text-body-3 text-ink-muted">
            {text}
          </pre>
        ) : (
          <div className="py-8 text-body-3 text-ink-muted">No preview for this file type. Use Download to open it.</div>
        )}
      </div>
    </Dialog>
  );
};
