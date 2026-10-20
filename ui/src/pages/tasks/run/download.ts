import { apiUrl } from "@/lib/http";
import { getToken } from "@/lib/session";

// Raw endpoints (files, transcript) sit outside the axios client, so they need
// the bearer token attached by hand. EventSource/<a href> can't, hence fetch.
async function fetchBlob(path: string): Promise<Blob> {
  const res = await fetch(apiUrl(path), { headers: { Authorization: `Bearer ${getToken() ?? ""}` } });
  if (!res.ok) throw new Error(`download failed (${res.status})`);
  return res.blob();
}

function save(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export function fetchTaskFileBlob(taskId: string, path: string): Promise<Blob> {
  return fetchBlob(`/tasks/${taskId}/files/${path}`);
}

export async function downloadTaskFile(taskId: string, path: string): Promise<void> {
  save(await fetchTaskFileBlob(taskId, path), path.split("/").pop() || path);
}

export async function downloadTranscript(taskId: string): Promise<void> {
  save(await fetchBlob(`/tasks/${taskId}/transcript`), `keen-task-${taskId}.txt`);
}
