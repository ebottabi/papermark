import { http } from "@/lib/http";
import type { ScheduledTask, ScheduleTriggerEntry } from "@/types";

export type NewSchedule = Partial<
  Pick<
    ScheduledTask,
    | "title"
    | "kind"
    | "prompt"
    | "mode"
    | "projectId"
    | "trigger"
    | "rule"
    | "repeat"
    | "time"
    | "weekday"
    | "maxRunsPerHour"
    | "enabled"
  >
>;

export const schedulesApi = {
  list: () =>
    http.get<{ schedules: ScheduledTask[] }>("/schedules").then((r) => r.schedules),
  create: (body: NewSchedule) => http.post<ScheduledTask>("/schedules", body),
  patch: (id: string, body: NewSchedule) =>
    http.patch<ScheduledTask>(`/schedules/${id}`, body),
  remove: (id: string) => http.del<void>(`/schedules/${id}`),
  run: (id: string) => http.post<{ id: string }>(`/schedules/${id}/run`),
  triggers: (id: string, limit = 30) =>
    http
      .get<{ triggers: ScheduleTriggerEntry[] }>(`/schedules/${id}/triggers`, {
        params: { limit },
      })
      .then((r) => r.triggers),
};
