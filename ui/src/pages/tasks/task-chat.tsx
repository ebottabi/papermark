import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Loader2 } from "lucide-react";
import { tasksApi } from "@/api/tasks";
import { ApiError } from "@/lib/http";
import { useTaskStream } from "@/hooks/use-task-stream";
import type { Decision } from "@/types";
import { Button, Skeleton, SkeletonRows, StatusBadge, Tooltip, toast } from "@/components/ui";
import { Composer } from "./composer";
import { foldTimeline, type Block } from "./fold-timeline";
import { Turn, KeenHeader } from "./run/turn";
import { Markdown } from "./run/markdown";
import { StatusStrip, type WaitingKind } from "./run/status-strip";
import { downloadTranscript } from "./run/download";

type Confirm = Extract<Block, { kind: "confirm" }>;

const ACTIVITY_LABEL: Record<string, string> = {
  thinking: "Thinking",
  web_search: "Searching the web",
  web_fetch: "Reading a page",
  shell_exec: "Running a command",
  file_read: "Reading a file",
  file_write: "Writing a file",
};

type Pending = { id: number; text: string; running: boolean };

const TaskChat = () => {
  const { taskId } = useParams<{ taskId: string }>();
  const qc = useQueryClient();
  const stream = useTaskStream(taskId);
  const bottomRef = useRef<HTMLDivElement>(null);
  const [pending, setPending] = useState<Pending[]>([]);

  const { data: task, isPending: taskLoading } = useQuery({
    queryKey: ["task", taskId],
    queryFn: () => tasksApi.get(taskId!),
    enabled: !!taskId,
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      return s === "running" || s === "waiting" ? 4000 : false;
    },
  });

  // Keep the rail's status chips fresh as this task progresses.
  useEffect(() => {
    if (task) qc.invalidateQueries({ queryKey: ["tasks"] });
  }, [task?.status, qc, task]);

  const timeline = useMemo(() => foldTimeline(stream.events.map((e) => e.event)), [stream.events]);

  const runningStepTitle = timeline.plan?.steps.find((s) => s.status === "running")?.title;

  const activeConfirm = useMemo<Confirm | null>(() => {
    if (task?.status !== "waiting") return null;
    for (let t = timeline.turns.length - 1; t >= 0; t--) {
      const blocks = timeline.turns[t].blocks;
      for (let b = blocks.length - 1; b >= 0; b--) {
        const block = blocks[b];
        if (block.kind === "confirm" && block.decision === null) return block;
      }
    }
    return null;
  }, [timeline.turns, task?.status]);

  const failureText = useMemo(() => {
    for (let i = stream.events.length - 1; i >= 0; i--) {
      const e = stream.events[i].event;
      if (e.type === "error") return e.text;
    }
    return "";
  }, [stream.events]);

  // Clear an optimistic bubble once the server echoes it back as a persisted event.
  useEffect(() => {
    if (!pending.length) return;
    const echoed = new Set<string>();
    for (const { event } of stream.events) {
      if (event.type === "message" || event.type === "reply") echoed.add(event.text);
    }
    setPending((p) => p.filter((x) => !echoed.has(x.text)));
  }, [stream.events, pending.length]);

  // Pin to the bottom instantly as tokens stream; a smooth animation here would
  // restart on every token and stutter. Smooth-scroll only on a new turn.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [stream.events.length, pending.length]);
  useEffect(() => {
    bottomRef.current?.scrollIntoView();
  }, [stream.delta, stream.activity]);

  const reply = useMutation({
    mutationFn: (text: string) => tasksApi.reply(taskId!, text),
    onError: (e) => toast.error("Couldn't send reply", e instanceof ApiError ? e.message : undefined),
  });
  const decide = useMutation({
    mutationFn: (decision: Decision) => tasksApi.confirm(taskId!, decision),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["task", taskId] }),
    onError: (e) => toast.error("Couldn't send decision", e instanceof ApiError ? e.message : undefined),
  });
  const message = useMutation({
    mutationFn: (text: string) => tasksApi.message(taskId!, text),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["task", taskId] }),
    onError: (e) => toast.error("Couldn't send message", e instanceof ApiError ? e.message : undefined),
  });
  const stop = useMutation({
    mutationFn: () => tasksApi.stop(taskId!),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["task", taskId] }),
    onError: (e) => toast.error("Couldn't stop task", e instanceof ApiError ? e.message : undefined),
  });

  if (!taskId) return null;

  const status = task?.status;
  const running = status === "running";
  const busy = reply.isPending || decide.isPending || message.isPending;
  const waiting: WaitingKind = status === "waiting" ? (activeConfirm ? "approval" : "reply") : null;
  const firstLoad = taskLoading && stream.events.length === 0;

  const send = (kind: "reply" | "message", text: string) => {
    setPending((p) => [...p, { id: Date.now() + Math.random(), text, running }]);
    (kind === "reply" ? reply : message).mutate(text);
  };

  const transcript = async () => {
    try {
      await downloadTranscript(taskId);
    } catch (e) {
      toast.error("Couldn't download transcript", e instanceof ApiError ? e.message : undefined);
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      {/* Header */}
      <header className="flex items-center gap-3 border-b border-gray-200 bg-background-surface px-4 py-3 sm:px-6">
        <div className="min-w-0 flex-1">
          {firstLoad ? (
            <Skeleton className="h-5 w-64" />
          ) : (
            <h1 className="truncate text-body-1 font-semibold text-ink">{task?.prompt || "Task"}</h1>
          )}
          <div className="mt-1 flex items-center gap-2">
            {task ? <StatusBadge status={task.status} /> : null}
            {!stream.connected ? (
              <span className="flex items-center gap-1 text-body-4 text-ink-faint">
                <Loader2 className="h-3 w-3 animate-spin" /> connecting…
              </span>
            ) : null}
          </div>
        </div>
        <Tooltip label="Download transcript">
          <Button variant="ghost" size="sm" onClick={transcript} aria-label="Download transcript">
            <Download className="h-4 w-4" />
          </Button>
        </Tooltip>
      </header>

      {/* Transcript */}
      <div className="scrollbar-thin flex-1 overflow-y-auto px-4 py-6 sm:px-6">
        <div className="mx-auto flex max-w-3xl flex-col gap-6">
          {firstLoad ? (
            <SkeletonRows rows={4} />
          ) : stream.events.length === 0 && !stream.delta ? (
            <div className="flex items-center gap-2 py-10 text-body-3 text-ink-faint">
              <Loader2 className="h-4 w-4 animate-spin" />
              Keen is getting started…
            </div>
          ) : (
            timeline.turns.map((turn, i) => (
              <div key={i} className="animate-step-in">
                <Turn
                  taskId={taskId}
                  turn={turn}
                  runningStepTitle={runningStepTitle}
                  autoOpenWork={i === timeline.turns.length - 1 && running}
                  activeConfirm={activeConfirm}
                  busy={busy}
                  terminal={i === timeline.turns.length - 1 && status === "done"}
                  onDecide={decide.mutate}
                />
              </div>
            ))
          )}

          {pending.map((p) => (
            <div key={p.id} className="flex flex-col items-end gap-0.5 opacity-55">
              <div className="max-w-[80%] rounded-2xl bg-accent px-4 py-2.5 text-body-2 leading-relaxed text-white">
                <p className="whitespace-pre-wrap">{p.text}</p>
              </div>
              <span className="text-body-4 text-ink-faint">{p.running ? "Queued" : "Sending"}</span>
            </div>
          ))}

          {stream.delta ? (
            <div className="flex flex-col gap-2.5">
              <KeenHeader />
              {/* Render the live stream as markdown so code and formatting
                  appear while Keen types, matching the persisted render. */}
              <div className="ml-9">
                <Markdown>{stream.delta}</Markdown>
                <span className="inline-block h-4 w-1.5 animate-blink bg-ink align-middle" />
              </div>
            </div>
          ) : stream.activity ? (
            <div className="ml-9 flex items-center gap-2 text-body-3 text-ink-faint">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              {ACTIVITY_LABEL[stream.activity] ?? stream.activity}…
            </div>
          ) : null}

          <div ref={bottomRef} />
        </div>
      </div>

      {/* Status + composer */}
      <div className="border-t border-gray-200 bg-background-surface px-4 py-3 sm:px-6">
        <div className="mx-auto max-w-3xl">
          {task ? (
            <StatusStrip
              status={task.status}
              plan={timeline.plan}
              runningStepTitle={runningStepTitle}
              waiting={waiting}
              failureText={failureText}
              stopping={stop.isPending}
              onStop={() => stop.mutate()}
            />
          ) : null}

          {waiting === "approval" ? (
            <Composer disabled showMode={false} placeholder="Waiting for your approval…" onSend={() => undefined} />
          ) : waiting === "reply" ? (
            <Composer
              autoFocus
              showMode={false}
              sending={reply.isPending}
              placeholder="Reply to Keen…"
              onSend={(text) => send("reply", text)}
            />
          ) : (
            <Composer
              showMode={false}
              sending={message.isPending}
              placeholder={running ? "Queue a message…" : "Continue this task…"}
              onSend={(text) => send("message", text)}
            />
          )}
        </div>
      </div>
    </div>
  );
};

export default TaskChat;
