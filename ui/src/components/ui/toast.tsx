import { useEffect, useState } from "react";
import * as ToastPrimitive from "@radix-ui/react-toast";
import { CheckCircle2, Info, X, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";

type ToastTone = "success" | "error" | "info";
type ToastMessage = { id: number; tone: ToastTone; title: string; description?: string };

const listeners = new Set<(t: ToastMessage) => void>();
let seq = 0;

function emit(tone: ToastTone, title: string, description?: string) {
  const msg = { id: ++seq, tone, title, description };
  listeners.forEach((l) => l(msg));
}

/** Fire a toast from anywhere (including non-React modules). */
export const toast = {
  success: (title: string, description?: string) => emit("success", title, description),
  error: (title: string, description?: string) => emit("error", title, description),
  info: (title: string, description?: string) => emit("info", title, description),
};

const TONE_META: Record<ToastTone, { icon: React.ReactNode; border: string }> = {
  success: { icon: <CheckCircle2 className="h-4 w-4 text-forest" />, border: "border-forest-border" },
  error: { icon: <XCircle className="h-4 w-4 text-crit" />, border: "border-crit-border" },
  info: { icon: <Info className="h-4 w-4 text-accent" />, border: "border-accent-border" },
};

export const ToastViewport = () => {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  useEffect(() => {
    const onMsg = (t: ToastMessage) => setToasts((prev) => [...prev, t]);
    listeners.add(onMsg);
    return () => void listeners.delete(onMsg);
  }, []);

  return (
    <ToastPrimitive.Provider swipeDirection="right" duration={4000}>
      {toasts.map((t) => (
        <ToastPrimitive.Root
          key={t.id}
          onOpenChange={(open) => !open && setToasts((prev) => prev.filter((x) => x.id !== t.id))}
          className={cn(
            "flex items-start gap-3 rounded-lg border bg-background-surface p-3 pr-2 shadow-dropdown",
            "data-[state=open]:animate-step-in",
            TONE_META[t.tone].border,
          )}
        >
          <span className="mt-0.5">{TONE_META[t.tone].icon}</span>
          <div className="min-w-0 flex-1">
            <ToastPrimitive.Title className="text-body-2 font-medium text-ink">{t.title}</ToastPrimitive.Title>
            {t.description ? (
              <ToastPrimitive.Description className="mt-0.5 text-body-3 text-ink-muted">
                {t.description}
              </ToastPrimitive.Description>
            ) : null}
          </div>
          <ToastPrimitive.Close aria-label="Dismiss" className="rounded p-1 text-ink-faint hover:bg-gray-100">
            <X className="h-3.5 w-3.5" />
          </ToastPrimitive.Close>
        </ToastPrimitive.Root>
      ))}
      <ToastPrimitive.Viewport className="fixed bottom-0 right-0 z-[100] m-4 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2 outline-none" />
    </ToastPrimitive.Provider>
  );
};
