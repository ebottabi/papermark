import { SESSION_TOKEN_KEY, SESSION_USER_KEY } from "./constants";
import { readStored, writeStored } from "./utils";
import type { User } from "@/types/auth";

// The Keen API authenticates every request with an opaque bearer token
// (see API.md "Auth"). We keep it, plus a cached User for instant UI, in
// localStorage. A subscriber list lets AuthProvider react to logout from
// anywhere (e.g. the http client clearing a dead token on 401).
type Listener = () => void;
const listeners = new Set<Listener>();

export function getToken(): string | null {
  return readStored<string | null>(SESSION_TOKEN_KEY, null);
}

export function getStoredUser(): User | null {
  return readStored<User | null>(SESSION_USER_KEY, null);
}

export function setSession(token: string, user: User): void {
  writeStored(SESSION_TOKEN_KEY, token);
  writeStored(SESSION_USER_KEY, user);
  listeners.forEach((l) => l());
}

export function clearSession(): void {
  try {
    window.localStorage.removeItem(SESSION_TOKEN_KEY);
    window.localStorage.removeItem(SESSION_USER_KEY);
  } catch {
    /* storage unavailable */
  }
  listeners.forEach((l) => l());
}

export function onSessionChange(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
