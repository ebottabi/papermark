import { cn } from "@/lib/utils";

export interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}

export const EmptyState = ({ icon, title, description, action, className }: EmptyStateProps) => (
  <div className={cn("flex flex-col items-center justify-center gap-3 px-6 py-16 text-center", className)}>
    {icon ? (
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 text-ink-faint">
        {icon}
      </div>
    ) : null}
    <div className="space-y-1">
      <h3 className="text-h-3 font-semibold text-ink">{title}</h3>
      {description ? <p className="mx-auto max-w-sm text-body-2 text-ink-muted">{description}</p> : null}
    </div>
    {action ? <div className="mt-2">{action}</div> : null}
  </div>
);

export interface InlineErrorProps {
  message: string;
  onRetry?: () => void;
}

export const InlineError = ({ message, onRetry }: InlineErrorProps) => (
  <div className="flex flex-col items-center gap-3 rounded-xl border border-crit-border bg-crit-soft px-6 py-10 text-center">
    <p className="text-body-2 text-crit">{message}</p>
    {onRetry ? (
      <button type="button" onClick={onRetry} className="text-body-3 font-medium text-accent-ink underline">
        Try again
      </button>
    ) : null}
  </div>
);
