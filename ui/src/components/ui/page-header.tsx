import { cn } from "@/lib/utils";

export interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: React.ReactNode;
  className?: string;
}

export const PageHeader = ({ title, description, actions, className }: PageHeaderProps) => (
  <div className={cn("mb-6 flex flex-wrap items-start justify-between gap-4", className)}>
    <div className="space-y-1">
      <h1 className="text-page-header font-semibold tracking-tight text-ink">{title}</h1>
      {description ? <p className="text-body-2 text-ink-muted">{description}</p> : null}
    </div>
    {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
  </div>
);
