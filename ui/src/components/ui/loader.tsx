import { cn } from "@/lib/utils";

export const Spinner = ({ className }: { className?: string }) => (
  <span
    role="status"
    aria-label="Loading"
    className={cn(
      "inline-block h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-accent",
      className,
    )}
  />
);

export const Loader = ({ label = "Loading…" }: { label?: string }) => (
  <div className="flex flex-col items-center justify-center gap-3 py-16 text-ink-faint">
    <Spinner className="h-6 w-6" />
    <p className="text-body-3">{label}</p>
  </div>
);
