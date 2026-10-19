import { http } from "@/lib/http";
import type { Connector } from "@/types";

export const connectorsApi = {
  list: () =>
    http.get<{ connectors: Connector[] }>("/connectors").then((r) => r.connectors),
  add: (body: { name: string; url: string }) => http.post<Connector>("/connectors", body),
  remove: (name: string) => http.del<void>(`/connectors/${name}`),
  /** Begin OAuth for a connector; returns a URL to open in a browser. */
  authorize: (name: string) =>
    http.post<{ authorizationUrl: string }>(`/connectors/${name}/authorize`),
  reconnectAccount: (kind: string, id: string) =>
    http.post<{ authorizationUrl: string }>(`/connectors/${kind}/accounts/${id}/authorize`),
  makePrimary: (kind: string, id: string) =>
    http.post<void>(`/connectors/${kind}/accounts/${id}/primary`),
  removeAccount: (kind: string, id: string) =>
    http.del<void>(`/connectors/${kind}/accounts/${id}`),
};
