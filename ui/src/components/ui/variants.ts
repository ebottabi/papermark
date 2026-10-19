import { cn } from "@/lib/utils";

export { cn };

export type Size = "sm" | "md" | "lg";
export type Tone = "neutral" | "accent" | "crit" | "high" | "forest" | "info";

export const focusRing =
  "outline-none focus-visible:ring-2 focus-visible:ring-accent/40 focus-visible:ring-offset-1 focus-visible:ring-offset-background-surface";

export const controlBase = cn(
  "w-full rounded-lg border border-gray-300 bg-background-surface",
  "text-body-2 text-ink placeholder:text-ink-faint",
  "transition-colors duration-150",
  "focus:border-accent focus:ring-2 focus:ring-accent/20 focus:outline-none",
  "disabled:cursor-not-allowed disabled:bg-gray-100 disabled:text-ink-faint",
);

export const controlSize: Record<Size, string> = {
  sm: "h-8 px-2.5 text-body-3",
  md: "h-10 px-3",
  lg: "h-11 px-3.5",
};

/** status key -> token family, for badges and dots. */
export const toneClasses: Record<Tone, { text: string; bg: string; border: string; dot: string }> =
  {
    neutral: { text: "text-ink-muted", bg: "bg-gray-100", border: "border-gray-300", dot: "bg-gray-500" },
    accent: { text: "text-accent-ink", bg: "bg-accent-soft", border: "border-accent-border", dot: "bg-accent" },
    crit: { text: "text-crit", bg: "bg-crit-soft", border: "border-crit-border", dot: "bg-crit" },
    high: { text: "text-high", bg: "bg-high-soft", border: "border-high-border", dot: "bg-high" },
    forest: { text: "text-forest", bg: "bg-forest-soft", border: "border-forest-border", dot: "bg-forest" },
    info: { text: "text-info-3", bg: "bg-info-1", border: "border-info-2", dot: "bg-info-3" },
  };
