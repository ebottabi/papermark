/**
 * Keen console palette — ported from the Sentry/Plane design system.
 * "Calm advisor, not neon cockpit": restrained warm neutrals, one cobalt
 * accent for active states and primary actions, and a disciplined status
 * vocabulary used only to communicate state, never as decoration.
 */

const colors = {
  // Structural ink ---------------------------------------------------------
  ink: {
    DEFAULT: "#191C22",
    soft: "#2A2E37",
    muted: "#565C68",
    faint: "#6B7180",
    deep: "#0F1116",
  },

  // The single brand accent (cobalt) --------------------------------------
  accent: {
    DEFAULT: "#2C51E0",
    hover: "#2545C4",
    pressed: "#1E39A6",
    soft: "#EEF1FE",
    softHover: "#E1E7FD",
    border: "#C9D4FA",
    ink: "#2C51E0",
  },

  // App canvas / surfaces --------------------------------------------------
  background: {
    DEFAULT: "#F5F6F8",
    surface: "#FFFFFF",
  },

  // Status vocabulary ------------------------------------------------------
  crit: { DEFAULT: "#B22A22", soft: "#FBEDEC", border: "#F0D2CF" },
  high: { DEFAULT: "#C67219", soft: "#FAF1E7", border: "#EFD9BF" },
  forest: { DEFAULT: "#2F7A57", soft: "#EBF4EF", border: "#CFE6D9" },

  gray: {
    50: "#FAFBFC",
    100: "#F5F6F8",
    200: "#EEF0F3",
    300: "#E7E9ED",
    400: "#D7DAE0",
    500: "#C9CDD4",
    600: "#6B7180",
    700: "#565C68",
    800: "#3D424C",
    900: "#2A2E37",
    1000: "#191C22",
  },

  success: { 1: "#EBF4EF", 2: "#CFE6D9", 3: "#2F7A57", 4: "#215A40" },
  danger: { hover: "#8E2018", 1: "#FBEDEC", 2: "#F0D2CF", 3: "#B22A22", 4: "#8E2018" },
  warning: { 1: "#FAF1E7", 2: "#EFD9BF", 3: "#C67219", 4: "#8A4E10" },
  info: { 1: "#EEF1FE", 2: "#C9D4FA", 3: "#2C51E0", 4: "#1E39A6" },
};

const rem = (px) => `${px / 16}rem`;

const fontSize = {
  "h-1": rem(24),
  "h-2": rem(20),
  "h-3": rem(16),
  "body-1": rem(16),
  "body-2": rem(14),
  "body-3": rem(12),
  "body-4": rem(10),
  "page-header": [26, 30].map(rem),
};

const fontFamily = {
  sans: ["Hanken Grotesk", "-apple-system", "Segoe UI", "sans-serif"],
  serif: ["Source Serif 4", "Georgia", "serif"],
  mono: ["IBM Plex Mono", "ui-monospace", "monospace"],
};

/** @type {import('tailwindcss').Config} */
export default {
  content: ["./src/**/*.{js,jsx,ts,tsx}", "./index.html"],
  theme: {
    extend: {
      colors,
      fontSize,
      fontFamily,
      borderRadius: { half: "9999px" },
      keyframes: {
        blink: { "50%": { opacity: "0" } },
        fadeIn: { from: { opacity: "0" }, to: { opacity: "1" } },
        dialogIn: {
          from: { opacity: "0", transform: "translateY(12px) scale(0.98)" },
          to: { opacity: "1", transform: "none" },
        },
        drawerIn: { from: { transform: "translateX(100%)" }, to: { transform: "none" } },
        overlayIn: { from: { opacity: "0" }, to: { opacity: "1" } },
        stepIn: {
          from: { opacity: "0", transform: "translateY(6px)" },
          to: { opacity: "1", transform: "none" },
        },
      },
      animation: {
        blink: "blink 1s step-end infinite",
        "fade-in": "fadeIn 200ms ease-out",
        "dialog-in": "dialogIn 240ms cubic-bezier(0.16, 1, 0.3, 1)",
        "drawer-in": "drawerIn 300ms cubic-bezier(0.16, 1, 0.3, 1)",
        "overlay-in": "overlayIn 200ms ease-out",
        "step-in": "stepIn 280ms cubic-bezier(0.16, 1, 0.3, 1) both",
      },
      boxShadow: {
        card: "0px 1px 2px rgba(25, 28, 34, 0.04), 0px 1px 3px rgba(25, 28, 34, 0.06)",
        "card-hover":
          "0px 4px 12px rgba(25, 28, 34, 0.08), 0px 2px 6px rgba(25, 28, 34, 0.06)",
        dropdown: "0px 4px 14px rgba(25, 28, 34, 0.10)",
      },
      transitionTimingFunction: {
        "out-expo": "cubic-bezier(0.16, 1, 0.3, 1)",
      },
    },
  },
  plugins: [],
};
