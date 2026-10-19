import type { Importance, UpdateSection, Usage } from "./task";

export * from "./auth";
export * from "./task";
export * from "./schedule";

export type Update = {
  id: string;
  taskId: string | null;
  scheduleId: string | null;
  importance: Importance;
  title: string;
  body: string;
  sections: UpdateSection[];
  read: boolean;
  createdAt: string;
};

export type Memory = {
  id: string;
  text: string;
  sourceTaskId: string | null;
  createdAt: string;
  updatedAt: string;
};

export type Preferences = {
  timezone: string;
  quietStart: string;
  quietEnd: string;
  pushLimit: number;
  newsTopics: string[];
  ntfyTopic: string | null;
};

export type Project = {
  id: string;
  name: string;
  instructions: string;
  createdAt: string;
};

export type Knowledge = {
  id: string;
  title: string;
  content: string;
  enabled: boolean;
  createdAt: string;
};

export type Skill = {
  id: string;
  name: string;
  description: string;
  instructions: string;
  createdAt: string;
};

export type ConnectorStatus = "connected" | "needs_auth" | "error";

export type GmailAccount = {
  id: string;
  email: string;
  status: ConnectorStatus;
  error: string | null;
  primary: boolean;
};

export type Connector = {
  name: string;
  kind: "mcp" | "gmail" | "calendar" | "contacts" | "drive";
  url: string;
  status: ConnectorStatus;
  error: string | null;
  tools: number;
  account: string | null;
  accounts: GmailAccount[];
};

export type UsageDay = { date: string; usage: Usage; tasks: number };
export type UsageReport = { days: UsageDay[]; total: Usage };
