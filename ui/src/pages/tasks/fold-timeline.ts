import type { AgentEvent, Decision, UpdateSection } from "@/types";

// A pure fold of the raw event log into the run view model, mirroring the iOS
// SessionState fold. Events in, structured turns out: no React, no I/O.

export type PlanStatus = "done" | "running" | "pending";
export type PlanStep = { title: string; status: PlanStatus };
export type Plan = { steps: PlanStep[]; done: number; total: number };

export type ToolItem = { name: string; input: unknown; output: string; isError: boolean };

export type UpdateBlock = {
  importance: "quiet" | "feed" | "push";
  title: string;
  body: string;
  sections: UpdateSection[];
};

export type Block =
  | { kind: "assistant"; text: string }
  | { kind: "work"; tools: ToolItem[] }
  | { kind: "confirm"; name: string; input: unknown; allowAlways: boolean; decision: Decision | null }
  | { kind: "result"; text: string; files: string[] }
  | { kind: "update"; update: UpdateBlock }
  | { kind: "notice"; tone: "neutral" | "crit"; text: string };

export type Turn = {
  user?: { text: string; attachments: string[] };
  blocks: Block[];
  skills: string[];
};

export type Timeline = { turns: Turn[]; plan: Plan | null };

// The tool whose job is to speak to the user; its text already arrives as a
// `notify`/`ask` event, so the tool call itself is noise in the timeline.
const NOTIFY_TOOL = "message_notify_user";

// Events the server may add that this tranche does not render: `skills` folds
// into per-turn chips, `card` (Trips) is dropped. Neither is in AgentEvent yet.
type SkillsEvent = { type: "skills"; name?: unknown; skill?: unknown; names?: unknown; skills?: unknown };
type CardEvent = { type: "card" };
type AnyEvent = AgentEvent | SkillsEvent | CardEvent;

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
}

function str(v: unknown): string | null {
  return typeof v === "string" && v.trim() ? v : null;
}

function toolPath(input: unknown): string | null {
  const r = asRecord(input);
  return str(r.path) ?? str(r.file_path) ?? str(r.file) ?? str(r.filename) ?? null;
}

function isFileWrite(name: string): boolean {
  return name === "file_write" || /write/i.test(name);
}

function isTodoWrite(name: string, input: unknown): boolean {
  if (!isFileWrite(name)) return false;
  const path = toolPath(input);
  return !!path && path.toLowerCase().endsWith("todo.md");
}

function todoContent(input: unknown): string | null {
  const r = asRecord(input);
  return str(r.content) ?? str(r.text) ?? str(r.data) ?? str(r.body) ?? null;
}

// Parse a markdown checklist: the first unchecked item is the running step.
function parsePlan(markdown: string): Plan | null {
  const steps: PlanStep[] = [];
  let runningMarked = false;
  for (const line of markdown.split("\n")) {
    const m = /^\s*[-*]\s*\[( |x|X)\]\s+(.*\S)\s*$/.exec(line);
    if (!m) continue;
    const checked = m[1].toLowerCase() === "x";
    const title = m[2];
    if (checked) {
      steps.push({ title, status: "done" });
    } else if (!runningMarked) {
      steps.push({ title, status: "running" });
      runningMarked = true;
    } else {
      steps.push({ title, status: "pending" });
    }
  }
  if (!steps.length) return null;
  return { steps, done: steps.filter((s) => s.status === "done").length, total: steps.length };
}

function skillNames(ev: SkillsEvent): string[] {
  const out: string[] = [];
  const push = (v: unknown) => {
    const s = str(v);
    if (s) out.push(s.trim());
  };
  push(ev.name);
  push(ev.skill);
  for (const v of [ev.names, ev.skills]) if (Array.isArray(v)) v.forEach(push);
  return out;
}

export function foldTimeline(events: AgentEvent[]): Timeline {
  if (!Array.isArray(events)) return { turns: [], plan: null };

  const turns: Turn[] = [];
  const confirms: { name: string; block: Extract<Block, { kind: "confirm" }> }[] = [];
  let latestTodo: string | null = null;

  const startTurn = (user?: Turn["user"]): Turn => {
    const turn: Turn = { user, blocks: [], skills: [] };
    turns.push(turn);
    return turn;
  };
  // Blocks before any user event (rare) still need a home.
  let current: Turn = startTurn();

  const addBlock = (block: Block) => current.blocks.push(block);

  for (const raw of events as AnyEvent[]) {
    if (!raw || typeof raw !== "object" || typeof raw.type !== "string") continue;
    const ev = raw;

    switch (ev.type) {
      case "message":
        current = startTurn({ text: ev.text ?? "", attachments: Array.isArray(ev.attachments) ? ev.attachments : [] });
        break;
      case "reply":
        current = startTurn({ text: ev.text ?? "", attachments: [] });
        break;
      case "notify":
      case "ask":
        if (str(ev.text)) addBlock({ kind: "assistant", text: ev.text });
        break;
      case "tool": {
        if (isTodoWrite(ev.name, ev.input)) {
          const content = todoContent(ev.input);
          if (content) latestTodo = content;
          break; // the todo write drives the plan, not the timeline
        }
        if (ev.name === NOTIFY_TOOL) break; // suppressed: its text is the notify event
        const last = current.blocks[current.blocks.length - 1];
        const item: ToolItem = { name: ev.name, input: ev.input, output: ev.output ?? "", isError: !!ev.isError };
        if (last && last.kind === "work") last.tools.push(item);
        else addBlock({ kind: "work", tools: [item] });
        break;
      }
      case "confirm": {
        const block: Extract<Block, { kind: "confirm" }> = {
          kind: "confirm",
          name: ev.name,
          input: ev.input,
          allowAlways: !!ev.allowAlways,
          decision: null,
        };
        confirms.push({ name: ev.name, block });
        addBlock(block);
        break;
      }
      case "decision": {
        // Resolve the most recent still-open confirm with this name.
        for (let i = confirms.length - 1; i >= 0; i--) {
          if (confirms[i].name === ev.name && confirms[i].block.decision === null) {
            confirms[i].block.decision = ev.decision;
            break;
          }
        }
        break;
      }
      case "idle":
        addBlock({ kind: "result", text: ev.text ?? "", files: Array.isArray(ev.files) ? ev.files : [] });
        break;
      case "update":
        addBlock({
          kind: "update",
          update: {
            importance: ev.importance,
            title: ev.title ?? "",
            body: ev.body ?? "",
            sections: Array.isArray(ev.sections) ? ev.sections : [],
          },
        });
        break;
      case "stopped":
        addBlock({ kind: "notice", tone: "neutral", text: str(ev.text) ?? "Task stopped." });
        break;
      case "error":
        addBlock({ kind: "notice", tone: "crit", text: str(ev.text) ?? "The task hit an error." });
        break;
      case "skills": {
        for (const name of skillNames(ev)) if (!current.skills.includes(name)) current.skills.push(name);
        break;
      }
      // "card" (Trips) and any unknown type are intentionally dropped.
      default:
        break;
    }
  }

  // Drop a leading empty turn left when the log opened with assistant content.
  const trimmed = turns.filter((t, i) => i > 0 || t.user || t.blocks.length || t.skills.length);
  return { turns: trimmed, plan: latestTodo ? parsePlan(latestTodo) : null };
}
