import * as Menu from "@radix-ui/react-dropdown-menu";
import { MoreHorizontal } from "lucide-react";
import { cn } from "@/lib/utils";
import { focusRing } from "./variants";

export type MenuAction = {
  label: string;
  onSelect: () => void;
  icon?: React.ReactNode;
  destructive?: boolean;
  disabled?: boolean;
};

export interface RowMenuProps {
  actions: MenuAction[];
  trigger?: React.ReactNode;
  align?: "start" | "end";
}

/** Overflow "⋯" menu for table rows and cards. */
export const RowMenu = ({ actions, trigger, align = "end" }: RowMenuProps) => (
  <Menu.Root>
    <Menu.Trigger asChild>
      {trigger ?? (
        <button
          type="button"
          aria-label="More actions"
          className={cn(
            "inline-flex h-8 w-8 items-center justify-center rounded-lg text-ink-faint transition-colors hover:bg-gray-100 hover:text-ink",
            focusRing,
          )}
        >
          <MoreHorizontal className="h-4 w-4" />
        </button>
      )}
    </Menu.Trigger>
    <Menu.Portal>
      <Menu.Content
        align={align}
        sideOffset={6}
        className="z-50 min-w-[180px] overflow-hidden rounded-lg border border-gray-200 bg-background-surface p-1 shadow-dropdown animate-fade-in"
      >
        {actions.map((a, i) => (
          <Menu.Item
            key={i}
            disabled={a.disabled}
            onSelect={a.onSelect}
            className={cn(
              "flex cursor-pointer select-none items-center gap-2 rounded-md px-2.5 py-1.5 text-body-2 outline-none",
              "data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50",
              a.destructive
                ? "text-crit data-[highlighted]:bg-crit-soft"
                : "text-ink data-[highlighted]:bg-gray-100",
            )}
          >
            {a.icon}
            {a.label}
          </Menu.Item>
        ))}
      </Menu.Content>
    </Menu.Portal>
  </Menu.Root>
);
