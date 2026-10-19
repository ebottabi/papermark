import { http } from "@/lib/http";
import type { Session, User } from "@/types";

export const authApi = {
  /** Google sign-in. Creates the account on first use. */
  google: (idToken: string) => http.post<Session>("/auth/google", { idToken }),
  me: () => http.get<User>("/me"),
  logout: () => http.post<{ ok: true }>("/auth/logout"),
  methods: () => http.get<{ passkey: boolean; faceId: boolean }>("/auth/methods"),
};
