export type Status = "running" | "waiting" | "done" | "stopped" | "error";
export type Mode = "speed" | "quality";
export type Decision = "allow" | "always" | "deny";
export type Importance = "quiet" | "feed" | "push";

export type Usage = {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
};

export type TaskSummary = {
  id: string;
  prompt: string;
  createdAt: string;
  updatedAt: string;
  status: Status;
  mode: Mode;
  projectId: string | null;
  scheduleId: string | null;
  favorite: boolean;
  archived: boolean;
  usage: Usage;
};

export type UpdateSection = {
  title: string;
  items: { text: string; detail?: string; url?: string }[];
};

// AgentEvent — the server replays these and streams new ones over SSE.
// Clients must ignore event types they do not know (API.md).
export type AgentEvent =
  | { type: "notify"; text: string }
  | { type: "ask"; text: string }
  | { type: "tool"; name: string; input: unknown; output: string; isError: boolean }
  | { type: "confirm"; name: string; input: unknown; allowAlways: boolean }
  | { type: "reply"; text: string }
  | { type: "decision"; name: string; decision: Decision }
  | { type: "message"; text: string; attachments: string[] }
  | { type: "idle"; text: string; files: string[] }
  | { type: "stopped"; text: string }
  | { type: "error"; text: string }
  | {
      type: "update";
      id: string;
      importance: Importance;
      title: string;
      body: string;
      sections: UpdateSection[];
    };

export type TaskDetail = TaskSummary & { events: AgentEvent[] };

export type TaskFile = { path: string; size: number; modifiedAt: string };

// Transport frames that aren't persisted AgentEvents: live typing + activity.
export type LiveFrame =
  | { kind: "event"; seq: number; event: AgentEvent }
  | { kind: "delta"; text: string }
  | { kind: "activity"; name: string };
