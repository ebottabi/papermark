import * as SwitchPrimitive from "@radix-ui/react-switch";
import { cn } from "@/lib/utils";

export interface SwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  "aria-label"?: string;
  className?: string;
}

export const Switch = ({ checked, onCheckedChange, disabled, className, ...aria }: SwitchProps) => (
  <SwitchPrimitive.Root
    checked={checked}
    onCheckedChange={onCheckedChange}
    disabled={disabled}
    aria-label={aria["aria-label"]}
    className={cn(
      "relative h-5 w-9 shrink-0 rounded-full border border-transparent transition-colors",
      "bg-gray-400 data-[state=checked]:bg-accent",
      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 focus-visible:ring-offset-1",
      "disabled:cursor-not-allowed disabled:opacity-50",
      className,
    )}
  >
    <SwitchPrimitive.Thumb className="block h-4 w-4 translate-x-0.5 rounded-full bg-white shadow transition-transform data-[state=checked]:translate-x-[18px]" />
  </SwitchPrimitive.Root>
);
