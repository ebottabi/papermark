import type { Mode } from "./task";

export type Trigger = "time" | "email" | "watch";
export type Repeat = "hourly" | "daily" | "weekdays" | "weekly" | null;

export type EmailRule = {
  account?: string;
  from?: string;
  subject?: string;
  label?: string;
  hasAttachment?: boolean;
  smart?: string;
};

export type WatchRule = {
  kind: "page" | "search";
  url?: string;
  query?: string;
  selector?: string;
  condition: "changed" | "below" | "above" | "contains" | "newItems";
  threshold?: number;
  text?: string;
  intervalMinutes: number;
};

export type ScheduledTask = {
  id: string;
  title: string;
  kind: "custom" | "brief";
  prompt: string;
  mode: Mode;
  projectId: string | null;
  trigger: Trigger;
  rule: EmailRule | WatchRule | null;
  repeat: Repeat;
  time: string | null;
  weekday: number | null;
  maxRunsPerHour: number;
  enabled: boolean;
  lastRunAt: string | null;
  nextRunAt: string | null;
  createdAt: string;
};

export type ScheduleTriggerEntry = {
  id: string;
  summary: string;
  taskId: string | null;
  createdAt: string;
};
