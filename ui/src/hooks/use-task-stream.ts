import { useEffect, useRef, useState } from "react";
import { apiUrl } from "@/lib/http";
import { getToken } from "@/lib/session";
import type { AgentEvent } from "@/types";

export type StreamState = {
  events: { seq: number; event: AgentEvent }[];
  delta: string; // live typing preview, cleared by the next persisted event
  activity: string | null; // "thinking" or a tool name
  connected: boolean;
};

// The SSE endpoint (API.md): persisted events carry `id: <seq>` and a JSON
// AgentEvent; `event: delta` frames preview streaming text (not persisted);
// `event: activity` frames name what the model is doing. EventSource can't set
// Authorization, so we read the stream with fetch and parse frames ourselves,
// resuming after the last seq with the Last-Event-ID header.
export function useTaskStream(taskId: string | undefined): StreamState {
  const [state, setState] = useState<StreamState>({
    events: [],
    delta: "",
    activity: null,
    connected: false,
  });
  const lastSeq = useRef(0);

  useEffect(() => {
    if (!taskId) return;
    lastSeq.current = 0;
    setState({ events: [], delta: "", activity: null, connected: false });

    const controller = new AbortController();
    let stopped = false;
    let retry = 0;

    const apply = (frame: { event?: string; id?: string; data: string }) => {
      let payload: unknown;
      try {
        payload = JSON.parse(frame.data);
      } catch {
        return;
      }
      if (frame.event === "delta") {
        setState((s) => ({ ...s, delta: s.delta + (payload as { text: string }).text }));
      } else if (frame.event === "activity") {
        setState((s) => ({ ...s, activity: (payload as { name: string }).name }));
      } else {
        const seq = frame.id ? Number(frame.id) : lastSeq.current + 1;
        if (frame.id) lastSeq.current = seq;
        // A new persisted event supersedes the live preview and activity.
        setState((s) => ({
          ...s,
          events: [...s.events, { seq, event: payload as AgentEvent }],
          delta: "",
          activity: null,
        }));
      }
    };

    const connect = async () => {
      try {
        const res = await fetch(apiUrl(`/tasks/${taskId}/events`), {
          headers: {
            Accept: "text/event-stream",
            Authorization: `Bearer ${getToken() ?? ""}`,
            ...(lastSeq.current ? { "Last-Event-ID": String(lastSeq.current) } : {}),
          },
          signal: controller.signal,
        });
        if (!res.ok || !res.body) throw new Error(`stream ${res.status}`);
        setState((s) => ({ ...s, connected: true }));
        retry = 0;

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          let idx: number;
          // Frames are separated by a blank line.
          while ((idx = buffer.indexOf("\n\n")) >= 0) {
            const block = buffer.slice(0, idx);
            buffer = buffer.slice(idx + 2);
            const frame: { event?: string; id?: string; data: string } = { data: "" };
            for (const line of block.split("\n")) {
              if (line.startsWith("id:")) frame.id = line.slice(3).trim();
              else if (line.startsWith("event:")) frame.event = line.slice(6).trim();
              else if (line.startsWith("data:")) frame.data += line.slice(5).trim();
            }
            if (frame.data) apply(frame);
          }
        }
      } catch {
        /* fall through to reconnect */
      }
      if (stopped) return;
      setState((s) => ({ ...s, connected: false }));
      retry = Math.min(retry + 1, 6);
      setTimeout(connect, 500 * retry);
    };

    connect();
    return () => {
      stopped = true;
      controller.abort();
    };
  }, [taskId]);

  return state;
}
