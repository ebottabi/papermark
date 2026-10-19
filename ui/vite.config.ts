import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

// The web console talks to the Go agent server. In dev we proxy /v1 and the
// SSE stream to it so the browser stays same-origin and EventSource works.
export default defineConfig(({ mode }) => {
  const apiTarget = process.env.VITE_API_PROXY ?? "http://localhost:8080";
  return {
    plugins: [react()],
    resolve: {
      alias: { "@": path.resolve(__dirname, "src") },
    },
    server: {
      port: 5173,
      proxy:
        mode === "development"
          ? { "/v1": { target: apiTarget, changeOrigin: true } }
          : undefined,
    },
  };
});
