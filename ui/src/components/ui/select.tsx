import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";
import { controlBase, controlSize, type Size } from "./variants";

export type SelectOption = { value: string; label: string };

export interface SelectProps {
  value?: string;
  onValueChange?: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  size?: Size;
  disabled?: boolean;
  className?: string;
  "aria-label"?: string;
}

export const Select = ({
  value,
  onValueChange,
  options,
  placeholder = "Select…",
  size = "md",
  disabled,
  className,
  ...aria
}: SelectProps) => (
  <SelectPrimitive.Root value={value} onValueChange={onValueChange} disabled={disabled}>
    <SelectPrimitive.Trigger
      aria-label={aria["aria-label"]}
      className={cn(controlBase, controlSize[size], "flex items-center justify-between gap-2", className)}
    >
      <SelectPrimitive.Value placeholder={placeholder} />
      <SelectPrimitive.Icon>
        <ChevronDown className="h-4 w-4 text-ink-faint" />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        position="popper"
        sideOffset={6}
        className="z-50 max-h-72 min-w-[--radix-select-trigger-width] overflow-hidden rounded-lg border border-gray-200 bg-background-surface shadow-dropdown animate-fade-in"
      >
        <SelectPrimitive.Viewport className="p-1">
          {options.map((o) => (
            <SelectPrimitive.Item
              key={o.value}
              value={o.value}
              className="relative flex cursor-pointer select-none items-center rounded-md py-1.5 pl-8 pr-3 text-body-2 text-ink outline-none data-[highlighted]:bg-accent-soft data-[highlighted]:text-accent-ink"
            >
              <SelectPrimitive.ItemIndicator className="absolute left-2">
                <Check className="h-4 w-4" />
              </SelectPrimitive.ItemIndicator>
              <SelectPrimitive.ItemText>{o.label}</SelectPrimitive.ItemText>
            </SelectPrimitive.Item>
          ))}
        </SelectPrimitive.Viewport>
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  </SelectPrimitive.Root>
);
