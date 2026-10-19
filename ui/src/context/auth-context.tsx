import { createContext, useEffect, useMemo, useState } from "react";
import { authApi } from "@/api/auth";
import { clearSession, getStoredUser, getToken, onSessionChange, setSession } from "@/lib/session";
import type { User } from "@/types";

type AuthState = {
  user: User | null;
  isAuthenticated: boolean;
  signInWithGoogle: (idToken: string) => Promise<User>;
  signInWithToken: (token: string) => Promise<User>;
  logout: () => void;
};

export const AuthContext = createContext<AuthState | null>(null);

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
  const [user, setUser] = useState<User | null>(() => (getToken() ? getStoredUser() : null));

  // Keep in sync if the token is cleared elsewhere (e.g. http 401 handler).
  useEffect(
    () =>
      onSessionChange(() => {
        if (!getToken()) setUser(null);
      }),
    [],
  );

  const signInWithGoogle = async (idToken: string) => {
    const session = await authApi.google(idToken);
    setSession(session.token, session.user);
    setUser(session.user);
    return session.user;
  };

  // Dev / headless fallback: paste a session token, verify it via /me.
  const signInWithToken = async (token: string) => {
    setSession(token, { id: "", email: "", name: "", picture: "", createdAt: "" });
    try {
      const me = await authApi.me();
      setSession(token, me);
      setUser(me);
      return me;
    } catch (err) {
      clearSession();
      setUser(null);
      throw err;
    }
  };

  const logout = () => {
    authApi.logout().catch(() => {});
    clearSession();
    setUser(null);
  };

  const value = useMemo<AuthState>(
    () => ({ user, isAuthenticated: !!user, signInWithGoogle, signInWithToken, logout }),
    [user],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};
