import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { GoogleLogin } from "@react-oauth/google";
import { useAuth } from "@/hooks/use-auth";
import config from "@/lib/config";
import { ROUTES } from "@/lib/constants";
import { ApiError } from "@/lib/http";
import { Loader, toast } from "@/components/ui";

const Login = () => {
  const { signInWithGoogle } = useAuth();
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);

  return (
    <div className="flex min-h-dvh items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3 text-center">
          <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-accent text-white">
            <span className="font-serif text-h-2 font-semibold">K</span>
          </div>
          <div>
            <h1 className="text-h-1 font-semibold text-ink">Welcome to Keen</h1>
            <p className="mt-1 text-body-2 text-ink-muted">Sign in to your agent workspace.</p>
          </div>
        </div>

        <div className="rounded-xl border border-gray-200 bg-background-surface p-6 shadow-card">
          {config.googleClientId ? (
            <div className="flex min-h-[40px] justify-center">
              {busy ? (
                <Loader label="Signing in…" />
              ) : (
                <GoogleLogin
                  onSuccess={async (res) => {
                    if (!res.credential) return;
                    setBusy(true);
                    try {
                      await signInWithGoogle(res.credential);
                      navigate(ROUTES.TASKS, { replace: true });
                    } catch (err) {
                      toast.error("Sign-in failed", err instanceof ApiError ? err.message : undefined);
                      setBusy(false);
                    }
                  }}
                  onError={() => toast.error("Google sign-in was cancelled")}
                />
              )}
            </div>
          ) : (
            <p className="rounded-lg bg-gray-100 p-3 text-body-3 text-ink-muted">
              Set <code className="font-mono">VITE_GOOGLE_CLIENT_ID</code> to enable Google sign-in.
            </p>
          )}
        </div>
      </div>
    </div>
  );
};

export default Login;
