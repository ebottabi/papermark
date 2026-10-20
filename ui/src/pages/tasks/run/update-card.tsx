import { cn } from "@/lib/utils";
import type { Tone } from "@/components/ui";
import { toneClasses } from "@/components/ui";
import type { UpdateBlock } from "../fold-timeline";
import { Markdown } from "./markdown";

// Importance sets how loudly the update's left marker reads.
const IMPORTANCE_TONE: Record<UpdateBlock["importance"], Tone> = {
  push: "accent",
  feed: "neutral",
  quiet: "neutral",
};

export const UpdateCard = ({ update }: { update: UpdateBlock }) => {
  const tone = IMPORTANCE_TONE[update.importance];
  return (
    <div className="ml-9 overflow-hidden rounded-lg border border-gray-200 bg-background-surface shadow-card">
      <div className="flex">
        <span className={cn("w-1 shrink-0", toneClasses[tone].dot, update.importance === "quiet" && "opacity-40")} />
        <div className="min-w-0 flex-1 p-4">
          {update.title ? <h3 className="text-body-1 font-semibold text-ink">{update.title}</h3> : null}
          {update.body ? <Markdown className="mt-1">{update.body}</Markdown> : null}
          {update.sections.map((section, i) => (
            <div key={i} className="mt-3">
              <p className="text-body-3 font-semibold text-ink">{section.title}</p>
              <ul className="mt-1 space-y-1">
                {section.items.map((item, j) => (
                  <li key={j} className="text-body-3 text-ink">
                    {item.url ? (
                      <a
                        href={item.url}
                        target="_blank"
                        rel="noreferrer"
                        className="font-medium text-accent-ink underline underline-offset-2"
                      >
                        {item.text}
                      </a>
                    ) : (
                      <span>{item.text}</span>
                    )}
                    {item.detail ? <span className="text-ink-muted"> · {item.detail}</span> : null}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
