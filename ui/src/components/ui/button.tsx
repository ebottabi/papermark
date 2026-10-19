import { forwardRef } from "react";
import { cn } from "@/lib/utils";
import { focusRing, type Size } from "./variants";
import { Spinner } from "./loader";

export type ButtonVariant =
  | "primary"
  | "accent"
  | "secondary"
  | "tertiary"
  | "danger"
  | "ghost";

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: Size;
  loading?: boolean;
  fullWidth?: boolean;
  leftIcon?: React.ReactNode;
  rightIcon?: React.ReactNode;
}

const sizes: Record<Size, string> = {
  sm: "h-8 px-3 text-body-3 gap-1.5",
  md: "h-10 px-4 text-body-2 gap-2",
  lg: "h-11 px-5 text-body-1 gap-2",
};

const variants: Record<ButtonVariant, string> = {
  primary: "bg-ink text-white hover:bg-ink-soft active:bg-ink-deep disabled:bg-gray-300",
  accent: "bg-accent text-white hover:bg-accent-hover active:bg-accent-pressed disabled:bg-accent/40",
  secondary:
    "bg-background-surface text-ink border border-gray-300 hover:bg-gray-50 active:bg-gray-100 disabled:text-ink-faint",
  tertiary:
    "bg-accent-soft text-accent-ink hover:bg-accent-softHover active:bg-accent-border disabled:text-accent/40",
  danger: "bg-crit text-white hover:bg-danger-hover active:bg-danger-4 disabled:bg-crit/40",
  ghost: "bg-transparent text-ink-muted hover:bg-gray-100 hover:text-ink active:bg-gray-200",
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { variant = "primary", size = "md", loading, fullWidth, leftIcon, rightIcon, className, children, disabled, ...rest },
    ref,
  ) => (
    <button
      ref={ref}
      disabled={disabled || loading}
      className={cn(
        "inline-flex items-center justify-center rounded-lg font-medium",
        "transition-colors duration-150 disabled:cursor-not-allowed",
        focusRing,
        sizes[size],
        variants[variant],
        fullWidth && "w-full",
        className,
      )}
      {...rest}
    >
      {loading ? <Spinner className="h-4 w-4 border-white/40 border-t-white" /> : leftIcon}
      {children}
      {!loading && rightIcon}
    </button>
  ),
);
Button.displayName = "Button";

/** Square icon-only button; pass an aria-label. */
export const IconButton = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ size = "md", className, ...rest }, ref) => (
    <Button
      ref={ref}
      size={size}
      className={cn("aspect-square px-0", size === "sm" ? "w-8" : size === "lg" ? "w-11" : "w-10", className)}
      {...rest}
    />
  ),
);
IconButton.displayName = "IconButton";

export default Button;
