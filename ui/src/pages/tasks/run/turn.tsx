import { memo } from "react";
import { Paperclip, Sparkles } from "lucide-react";
import type { Decision } from "@/types";
import type { Block, Turn as TurnModel } from "../fold-timeline";
import { Markdown } from "./markdown";
import { WorkGroup } from "./work-group";
import { ResultCard } from "./result-card";
import { UpdateCard } from "./update-card";
import { ApprovalCard } from "./approval-card";
import { AgentLanes } from "./agent-lanes";

type Confirm = Extract<Block, { kind: "confirm" }>;

export const KeenHeader = () => (
  <div className="flex items-center gap-2">
    <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-accent text-white">
      <span className="font-serif text-body-1 font-semibold">K</span>
    </div>
    <span className="text-body-3 font-semibold text-ink">Keen</span>
  </div>
);

const UserBubble = ({ text, attachments }: { text: string; attachments: string[] }) => (
  <div className="flex justify-end">
    <div className="max-w-[80%] rounded-2xl bg-accent px-4 py-2.5 text-body-2 leading-relaxed text-white">
      <p className="whitespace-pre-wrap">{text}</p>
      {attachments.length ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {attachments.map((a) => (
            <span key={a} className="inline-flex items-center gap-1 rounded-md bg-white/15 px-1.5 py-0.5 text-body-4">
              <Paperclip className="h-3 w-3" />
              {a.split("/").pop() || a}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  </div>
);

const SkillChips = ({ skills }: { skills: string[] }) => (
  <div className="ml-9 flex flex-wrap gap-1.5">
    {skills.map((s) => (
      <span
        key={s}
        className="inline-flex items-center gap-1 rounded-full border border-accent-border bg-accent-soft px-2 py-0.5 text-body-4 text-accent-ink"
      >
        <Sparkles className="h-3 w-3" />
        {s}
      </span>
    ))}
  </div>
);

const BlockView = ({
  taskId,
  block,
  runningStepTitle,
  autoOpenWork,
  activeConfirm,
  busy,
  completed,
  onDecide,
}: {
  taskId: string;
  block: Block;
  runningStepTitle?: string;
  autoOpenWork: boolean;
  activeConfirm: Confirm | null;
  busy: boolean;
  completed: boolean;
  onDecide: (d: Decision) => void;
}) => {
  switch (block.kind) {
    case "assistant":
      return <Markdown className="ml-9">{block.text}</Markdown>;
    case "work":
      return <WorkGroup tools={block.tools} runningStepTitle={runningStepTitle} defaultOpen={autoOpenWork} />;
    case "confirm":
      return <ApprovalCard confirm={block} active={block === activeConfirm} busy={busy} onDecide={onDecide} />;
    case "result":
      return <ResultCard taskId={taskId} text={block.text} files={block.files} completed={completed} />;
    case "update":
      return <UpdateCard update={block.update} />;
    case "lanes":
      return <AgentLanes lanes={block.lanes} />;
    case "notice":
      return (
        <div
          className={
            block.tone === "crit"
              ? "ml-9 rounded-lg border border-crit-border bg-crit-soft px-3 py-2 text-body-3 text-crit"
              : "ml-9 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-body-3 text-ink-muted"
          }
        >
          {block.text}
        </div>
      );
  }
};

export const Turn = memo(({
  taskId,
  turn,
  runningStepTitle,
  autoOpenWork,
  activeConfirm,
  busy,
  terminal,
  onDecide,
}: {
  taskId: string;
  turn: TurnModel;
  runningStepTitle?: string;
  autoOpenWork: boolean;
  activeConfirm: Confirm | null;
  busy: boolean;
  terminal: boolean;
  onDecide: (d: Decision) => void;
}) => (
  <div className="flex flex-col gap-3">
    {turn.user ? <UserBubble text={turn.user.text} attachments={turn.user.attachments} /> : null}
    {turn.blocks.length || turn.skills.length ? (
      <div className="flex flex-col gap-2.5">
        <KeenHeader />
        {turn.skills.length ? <SkillChips skills={turn.skills} /> : null}
        {turn.blocks.map((block, i) => (
          <BlockView
            key={i}
            taskId={taskId}
            block={block}
            runningStepTitle={runningStepTitle}
            autoOpenWork={autoOpenWork}
            activeConfirm={activeConfirm}
            busy={busy}
            completed={terminal && i === turn.blocks.length - 1}
            onDecide={onDecide}
          />
        ))}
      </div>
    ) : null}
  </div>
));
