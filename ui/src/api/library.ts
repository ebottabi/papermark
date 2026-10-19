import { http } from "@/lib/http";
import type {
  Knowledge,
  Memory,
  Preferences,
  Project,
  Skill,
  Update,
  UsageReport,
} from "@/types";

export const updatesApi = {
  list: (params: { before?: string; limit?: number } = {}) =>
    http.get<{ updates: Update[] }>("/updates", { params }).then((r) => r.updates),
  read: (id: string) => http.post<void>(`/updates/${id}/read`),
  readAll: () => http.post<void>("/updates/read-all"),
};

export const memoriesApi = {
  list: () => http.get<{ memories: Memory[] }>("/memories").then((r) => r.memories),
  create: (text: string) => http.post<Memory>("/memories", { text }),
  patch: (id: string, text: string) => http.patch<Memory>(`/memories/${id}`, { text }),
  remove: (id: string) => http.del<void>(`/memories/${id}`),
};

export const projectsApi = {
  list: () => http.get<Project[]>("/projects"),
  create: (body: { name: string; instructions: string }) =>
    http.post<Project>("/projects", body),
  patch: (id: string, body: Partial<Pick<Project, "name" | "instructions">>) =>
    http.patch<Project>(`/projects/${id}`, body),
  remove: (id: string) => http.del<void>(`/projects/${id}`),
};

export const knowledgeApi = {
  list: () => http.get<Knowledge[]>("/knowledge"),
  create: (body: { title: string; content: string; enabled?: boolean }) =>
    http.post<Knowledge>("/knowledge", body),
  patch: (id: string, body: Partial<Pick<Knowledge, "title" | "content" | "enabled">>) =>
    http.patch<Knowledge>(`/knowledge/${id}`, body),
  remove: (id: string) => http.del<void>(`/knowledge/${id}`),
};

export const skillsApi = {
  list: () => http.get<Skill[]>("/skills"),
  create: (body: { name: string; description: string; instructions: string }) =>
    http.post<Skill>("/skills", body),
  patch: (id: string, body: Partial<Pick<Skill, "name" | "description" | "instructions">>) =>
    http.patch<Skill>(`/skills/${id}`, body),
  remove: (id: string) => http.del<void>(`/skills/${id}`),
};

export const preferencesApi = {
  get: () => http.get<Preferences>("/me/preferences"),
  patch: (body: Partial<Preferences>) => http.patch<Preferences>("/me/preferences", body),
};

export const usageApi = {
  get: (days = 30) => http.get<UsageReport>("/usage", { params: { days } }),
};
