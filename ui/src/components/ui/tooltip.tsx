import * as TooltipPrimitive from "@radix-ui/react-tooltip";

export const TooltipProvider = TooltipPrimitive.Provider;

export const Tooltip = ({ label, children }: { label: string; children: React.ReactNode }) => (
  <TooltipPrimitive.Root>
    <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        sideOffset={6}
        className="z-50 rounded-md bg-ink px-2 py-1 text-body-4 text-white shadow-dropdown animate-fade-in"
      >
        {label}
        <TooltipPrimitive.Arrow className="fill-ink" />
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  </TooltipPrimitive.Root>
);
