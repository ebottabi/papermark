import { cn } from "@/lib/utils";

export interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  interactive?: boolean;
}

export const Card = ({ interactive, className, ...rest }: CardProps) => (
  <div
    className={cn(
      "rounded-xl border border-gray-200 bg-background-surface shadow-card",
      interactive && "cursor-pointer transition-shadow hover:shadow-card-hover",
      className,
    )}
    {...rest}
  />
);

export interface StatCardProps {
  label: string;
  value: React.ReactNode;
  sub?: string;
  icon?: React.ReactNode;
}

export const StatCard = ({ label, value, sub, icon }: StatCardProps) => (
  <Card className="p-4">
    <div className="flex items-start justify-between">
      <p className="text-body-3 text-ink-muted">{label}</p>
      {icon ? <span className="text-ink-faint">{icon}</span> : null}
    </div>
    <p className="mt-2 font-mono text-h-1 font-semibold tabular-nums text-ink">{value}</p>
    {sub ? <p className="mt-1 text-body-3 text-ink-faint">{sub}</p> : null}
  </Card>
);
