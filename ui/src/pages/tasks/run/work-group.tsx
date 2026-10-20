import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui";
import type { ToolItem } from "../fold-timeline";
import { toolMeta } from "../tool-meta";

/** Keep the head and tail visible, eliding the middle: "/long/pa…/file.ts". */
function middleTruncate(text: string, max = 56): string {
  if (text.length <= max) return text;
  const keep = max - 1;
  const head = Math.ceil(keep / 2);
  const tail = Math.floor(keep / 2);
  return `${text.slice(0, head)}…${text.slice(text.length - tail)}`;
}

function prettyInput(input: unknown): string {
  if (input == null) return "";
  if (typeof input === "string") return input;
  try {
    return JSON.stringify(input, null, 2);
  } catch {
    return String(input);
  }
}

// A file write carries the document in `content`; JSON.stringify would collapse
// its newlines into one escaped line, so render that field raw and readable.
function fileContent(input: unknown): string | null {
  if (input && typeof input === "object" && !Array.isArray(input)) {
    const c = (input as Record<string, unknown>).content;
    if (typeof c === "string") return c;
  }
  return null;
}

const ToolRow = ({ tool }: { tool: ToolItem }) => {
  const [open, setOpen] = useState(false);
  const meta = toolMeta(tool.name, tool.input);
  const Icon = meta.icon;
  const content = fileContent(tool.input);
  return (
    <div className="border-t border-gray-200 first:border-t-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2.5 py-2 pl-3 pr-2 text-left hover:bg-gray-50"
      >
        <Icon className="h-4 w-4 shrink-0" style={meta.accentHex ? { color: meta.accentHex } : undefined} aria-hidden />
        <span className="shrink-0 text-body-3 text-ink">{meta.title}</span>
        {meta.subject ? (
          <span className={cn("min-w-0 truncate text-body-3 text-ink-faint", meta.mono && "font-mono")}>
            {middleTruncate(meta.subject)}
          </span>
        ) : null}
        {tool.isError ? <Badge tone="crit">failed</Badge> : null}
        <ChevronRight className={cn("ml-auto h-4 w-4 shrink-0 text-ink-faint transition-transform", open && "rotate-90")} />
      </button>
      {open ? (
        <div className="space-y-2 px-3 pb-3">
          {content != null ? (
            <pre className="scrollbar-thin max-h-96 overflow-auto whitespace-pre-wrap rounded-md border border-gray-200 bg-gray-50 p-2.5 font-mono text-body-3 text-ink-muted">
              {content || "(empty file)"}
            </pre>
          ) : meta.subject || tool.input ? (
            <pre className="scrollbar-thin max-h-48 overflow-auto rounded-md border border-gray-200 bg-gray-50 p-2.5 font-mono text-body-3 text-ink-muted">
              {prettyInput(tool.input) || "(no input)"}
            </pre>
          ) : null}
          <pre className="scrollbar-thin max-h-72 overflow-auto rounded-md border border-gray-200 bg-gray-50 p-2.5 font-mono text-body-3 text-ink-muted">
            {tool.output || "(no output)"}
          </pre>
        </div>
      ) : null}
    </div>
  );
};

export const WorkGroup = ({
  tools,
  runningStepTitle,
  defaultOpen = false,
}: {
  tools: ToolItem[];
  runningStepTitle?: string;
  defaultOpen?: boolean;
}) => {
  const [open, setOpen] = useState(defaultOpen);
  if (!tools.length) return null;
  const header = runningStepTitle || "Working";

  return (
    <div className="ml-9 rounded-lg border border-gray-200 bg-background-surface">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left"
      >
        <ChevronRight className={cn("h-4 w-4 shrink-0 text-ink-faint transition-transform", open && "rotate-90")} />
        <span className="min-w-0 truncate text-body-3 font-medium text-ink">{header}</span>
        <span className="ml-auto shrink-0 font-mono text-body-4 text-ink-faint">
          {tools.length} {tools.length === 1 ? "step" : "steps"}
        </span>
      </button>
      {open ? <div className="border-t border-gray-200">{tools.map((t, i) => <ToolRow key={i} tool={t} />)}</div> : null}
    </div>
  );
};
