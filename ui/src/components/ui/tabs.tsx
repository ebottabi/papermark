import * as TabsPrimitive from "@radix-ui/react-tabs";
import { cn } from "@/lib/utils";

export type TabItem = { value: string; label: string; count?: number };

export interface TabsProps {
  value: string;
  onValueChange: (value: string) => void;
  items: TabItem[];
  className?: string;
}

/** Underline-style segmented tabs matching the reference FilterTabs. */
export const Tabs = ({ value, onValueChange, items, className }: TabsProps) => (
  <TabsPrimitive.Root value={value} onValueChange={onValueChange}>
    <TabsPrimitive.List className={cn("flex items-center gap-1 border-b border-gray-200", className)}>
      {items.map((t) => (
        <TabsPrimitive.Trigger
          key={t.value}
          value={t.value}
          className={cn(
            "relative -mb-px flex items-center gap-1.5 px-3 py-2 text-body-2 font-medium transition-colors",
            "text-ink-muted hover:text-ink",
            "data-[state=active]:text-ink data-[state=active]:after:absolute data-[state=active]:after:inset-x-0 data-[state=active]:after:-bottom-px data-[state=active]:after:h-0.5 data-[state=active]:after:rounded-full data-[state=active]:after:bg-accent",
          )}
        >
          {t.label}
          {typeof t.count === "number" ? (
            <span className="rounded-full bg-gray-100 px-1.5 text-body-4 text-ink-faint">{t.count}</span>
          ) : null}
        </TabsPrimitive.Trigger>
      ))}
    </TabsPrimitive.List>
  </TabsPrimitive.Root>
);
