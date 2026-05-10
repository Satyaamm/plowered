"use client";

import { useEffect, useRef, useState } from "react";

export interface RunLogLine {
  id: number;
  task_id?: string;
  task_run_id?: string;
  level: string;
  line: string;
  created_at: string;
}

interface State {
  lines: RunLogLine[];
  status: "idle" | "open" | "done" | "error";
  error?: string;
}

/**
 * useRunLogs streams Server-Sent Events from /v1/runs/{id}/logs/stream.
 * The endpoint emits one event per log line and a terminal "done" event
 * when the run reaches a terminal state. We buffer lines locally so the
 * UI re-renders on every tick without refetching the whole list.
 *
 * Cookie auth flows through automatically because EventSource always
 * sends same-origin cookies; the rewrite in next.config.ts proxies to
 * the API while preserving the plowered_session cookie.
 */
export function useRunLogs(runId: string | undefined) {
  const [state, setState] = useState<State>({ lines: [], status: "idle" });
  const sinceRef = useRef<number>(0);

  useEffect(() => {
    if (!runId) return;
    setState({ lines: [], status: "open" });
    sinceRef.current = 0;

    const url = `/api/v1/runs/${encodeURIComponent(runId)}/logs/stream`;
    const es = new EventSource(url, { withCredentials: true });

    es.onmessage = (ev) => {
      try {
        const parsed = JSON.parse(ev.data) as RunLogLine;
        if (parsed.id <= sinceRef.current) return;
        sinceRef.current = parsed.id;
        setState((s) => ({ ...s, lines: [...s.lines, parsed] }));
      } catch {
        /* ignore malformed payload */
      }
    };

    es.addEventListener("done", () => {
      setState((s) => ({ ...s, status: "done" }));
      es.close();
    });

    es.addEventListener("error", () => {
      // EventSource auto-reconnects on most errors; only surface on close.
      if (es.readyState === EventSource.CLOSED) {
        setState((s) => ({ ...s, status: "error", error: "stream closed" }));
      }
    });

    return () => {
      es.close();
    };
  }, [runId]);

  return state;
}
