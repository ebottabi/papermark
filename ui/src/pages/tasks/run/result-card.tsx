import { useState } from "react";
import { CheckCircle2, FileText, PauseCircle } from "lucide-react";
import { Markdown } from "./markdown";
import { FileViewer } from "./file-viewer";

// `completed` is the run's final result. An idle that fires mid-task is a pause,
// not a completion, so the marker must not claim the task is done.
export const ResultCard = ({
  taskId,
  text,
  files,
  completed,
}: {
  taskId: string;
  text: string;
  files: string[];
  completed: boolean;
}) => {
  const [viewing, setViewing] = useState<string | null>(null);

  return (
    <div className="ml-9 rounded-lg border border-gray-200 bg-background-surface p-4 shadow-card">
      {completed ? (
        <div className="mb-2 flex items-center gap-1.5 text-body-4 font-medium text-forest">
          <CheckCircle2 className="h-3.5 w-3.5" />
          Task completed
        </div>
      ) : (
        <div className="mb-2 flex items-center gap-1.5 text-body-4 font-medium text-ink-muted">
          <PauseCircle className="h-3.5 w-3.5" />
          Keen paused
        </div>
      )}
      {text ? <Markdown>{text}</Markdown> : null}
      {files.length ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {files.map((f) => (
            <button
              key={f}
              type="button"
              onClick={() => setViewing(f)}
              className="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 bg-background-surface px-2.5 py-1.5 text-body-3 text-ink hover:bg-gray-50"
            >
              <FileText className="h-3.5 w-3.5 text-ink-faint" />
              <span className="font-mono">{f.split("/").pop() || f}</span>
            </button>
          ))}
        </div>
      ) : null}
      {viewing ? <FileViewer taskId={taskId} path={viewing} onClose={() => setViewing(null)} /> : null}
    </div>
  );
};
