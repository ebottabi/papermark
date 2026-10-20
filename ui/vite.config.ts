import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// The web console talks to the Go agent server. In dev we proxy /v1 and the
// SSE stream to it so the browser stays same-origin and EventSource works.
export default defineConfig(({ mode }) => {
  const apiTarget = process.env.VITE_API_PROXY ?? "https://papermark.tracepipe.xyz";
  return {
    plugins: [react()],
    resolve: {
      alias: { "@": path.resolve(__dirname, "src") },
    },
    server: {
      port: 5174,
      // Google Sign-In opens a popup and postMessages back; the default COOP
      // blocks that, surfacing as a cross-origin error. Allow popups to talk back.
      headers: { "Cross-Origin-Opener-Policy": "same-origin-allow-popups" },
      proxy:
        mode === "development"
          ? { "/v1": { target: apiTarget, changeOrigin: true } }
          : undefined,
    },
  };
});
