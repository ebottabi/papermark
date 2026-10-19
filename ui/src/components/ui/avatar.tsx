import * as AvatarPrimitive from "@radix-ui/react-avatar";
import { cn } from "@/lib/utils";
import { initials } from "@/lib/utils";

export interface AvatarProps {
  name: string;
  src?: string;
  size?: "sm" | "md" | "lg";
  className?: string;
}

const sizes = { sm: "h-7 w-7 text-body-4", md: "h-9 w-9 text-body-3", lg: "h-11 w-11 text-body-2" };

export const Avatar = ({ name, src, size = "md", className }: AvatarProps) => (
  <AvatarPrimitive.Root
    className={cn(
      "inline-flex shrink-0 select-none items-center justify-center overflow-hidden rounded-full",
      sizes[size],
      className,
    )}
  >
    {src ? <AvatarPrimitive.Image src={src} alt={name} className="h-full w-full object-cover" /> : null}
    <AvatarPrimitive.Fallback className="flex h-full w-full items-center justify-center bg-accent-soft font-medium text-accent-ink">
      {initials(name)}
    </AvatarPrimitive.Fallback>
  </AvatarPrimitive.Root>
);
