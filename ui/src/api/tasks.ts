import { http } from "@/lib/http";
import type { Decision, TaskDetail, TaskFile, TaskSummary } from "@/types";

export type NewTask = {
  task: string;
  mode?: "speed" | "quality";
  projectId?: string;
  attachments?: { name: string; base64: string }[];
};

export const tasksApi = {
  list: (params: { archived?: boolean; projectId?: string } = {}) =>
    http
      .get<{ tasks: TaskSummary[] }>("/tasks", { params })
      .then((r) => r.tasks),
  get: (id: string) => http.get<TaskDetail>(`/tasks/${id}`),
  create: (body: NewTask) => http.post<{ id: string }>("/tasks", body),
  patch: (id: string, body: { favorite?: boolean; archived?: boolean }) =>
    http.patch<TaskSummary>(`/tasks/${id}`, body),
  reply: (id: string, text: string) => http.post<void>(`/tasks/${id}/reply`, { text }),
  confirm: (id: string, decision: Decision) =>
    http.post<void>(`/tasks/${id}/confirm`, { decision }),
  message: (id: string, text: string) =>
    http.post<void>(`/tasks/${id}/messages`, { text }),
  stop: (id: string) => http.post<void>(`/tasks/${id}/stop`),
  fork: (id: string, task: string) => http.post<{ id: string }>(`/tasks/${id}/fork`, { task }),
  files: (id: string) =>
    http.get<{ files: TaskFile[] }>(`/tasks/${id}/files`).then((r) => r.files),
};
