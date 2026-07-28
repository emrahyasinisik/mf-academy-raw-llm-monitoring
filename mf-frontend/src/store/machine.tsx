"use client";

// What the app knows about the machine it drives.
//
// The product is a front end for one specific inference host, and that host is a
// desktop GPU behind a tunnel: it is sometimes simply off. Before this store,
// every view discovered that on its own and said so in its own words, which is
// why the interface used to read like a workshop log ("tüneli ve mlc servisini
// kontrol et"). Liveness is one fact about one machine, so it is held once and
// rendered once, by the status rail.
//
// The store deliberately does not poll. A reachability check costs a request to
// a host that serves one generation at a time, and a background poll competing
// with a running job is the last thing this hardware needs. It resolves on
// mount, and every completed generation refreshes it for free — a run that
// returned is the strongest possible evidence the host is up.

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useEffect } from "react";
import { api } from "@/lib/api";
import { useAuth } from "@/store/auth";
import type { ModelInfo, Run } from "@/lib/types";

export type HostState = "checking" | "online" | "offline";

/** What the last completed generation cost. Shown as the rail's resting state. */
export interface LastRun {
  tokens: number;
  seconds: number;
  tokensPerSecond: number;
  model: string;
}

interface MachineState {
  host: HostState;
  /** Label of the model the host will serve, as the backend reports it. */
  model: string;
  /**
   * Every catalogue entry the host can actually serve.
   *
   * Held here rather than fetched by the view that needs a picker, because the
   * rail already asks this exact question to decide whether the host is up —
   * and two components asking it on mount is two requests to a machine that
   * serves one at a time.
   */
  models: ModelInfo[];
  /** What the machine is doing right now, or null when idle. */
  activity: string | null;
  /** Epoch ms the current activity began; the rail counts up from it. */
  startedAt: number | null;
  lastRun: LastRun | null;
  /** Mark the machine busy. Returns a function that marks this job done. */
  begin: (label: string) => (run?: Run | null) => void;
  refresh: () => void;
}

const MachineContext = createContext<MachineState | null>(null);

export function MachineProvider({ children }: { children: ReactNode }) {
  // Nothing is probed until somebody is signed in. /llm/models is authenticated,
  // so mounting this unconditionally fired a request on the login screen that
  // could only ever come back 401 — and then painted the host as offline on the
  // strength of it, before the session had even been established.
  const { user } = useAuth();
  const [host, setHost] = useState<HostState>("checking");
  const [model, setModel] = useState("");
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [activity, setActivity] = useState<string | null>(null);
  const [startedAt, setStartedAt] = useState<number | null>(null);
  const [lastRun, setLastRun] = useState<LastRun | null>(null);

  // Two views stay mounted at once and can both be generating. A counter rather
  // than a boolean, so the first one to finish does not report the machine idle
  // while the second is still running.
  const jobs = useRef(0);

  const refresh = useCallback(() => {
    if (!user) return;
    api
      .models()
      .then((res) => {
        // Server-capable only. The adapter is compiled into the self-hosted
        // build, so a browser target would silently serve a different model
        // that has never seen the project's code standard.
        const servable = res.models.filter((m) => m.targets.includes("server"));
        const preferred = servable.find((m) => m.recommended) ?? servable[0];
        setModels(servable);
        setModel(preferred?.label ?? "");
        setHost(res.server_inference && servable.length > 0 ? "online" : "offline");
      })
      .catch(() => {
        setModels([]);
        setHost("offline");
      });
  }, [user]);

  useEffect(refresh, [refresh]);

  const begin = useCallback((label: string) => {
    jobs.current += 1;
    setActivity(label);
    setStartedAt(Date.now());

    let settled = false;
    return (run?: Run | null) => {
      // Guards a caller that finishes twice — a retry path calling its own
      // finisher again would otherwise drive the counter negative and leave the
      // rail stuck on "working" forever.
      if (settled) return;
      settled = true;
      jobs.current = Math.max(0, jobs.current - 1);
      if (jobs.current === 0) {
        setActivity(null);
        setStartedAt(null);
      }
      if (run) {
        const seconds = run.latency_ms / 1000;
        setLastRun({
          tokens: run.completion_tokens,
          seconds,
          tokensPerSecond: seconds > 0 ? run.completion_tokens / seconds : 0,
          model: run.model,
        });
        // A run came back, so the host is up whatever the last probe concluded.
        setHost("online");
      }
    };
  }, []);

  const value = useMemo(
    () => ({ host, model, models, activity, startedAt, lastRun, begin, refresh }),
    [host, model, models, activity, startedAt, lastRun, begin, refresh],
  );

  return <MachineContext.Provider value={value}>{children}</MachineContext.Provider>;
}

export function useMachine(): MachineState {
  const ctx = useContext(MachineContext);
  if (!ctx) throw new Error("useMachine must be used within MachineProvider");
  return ctx;
}
