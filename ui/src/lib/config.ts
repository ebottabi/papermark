// Base URL for the Keen agent server. In dev we leave it as "/" and let Vite
// proxy /v1 to the Go server (see vite.config.ts); in prod set VITE_API_BASE_URL
// to the API origin. The http client appends "v1/<path>".
const baseUrl = import.meta.env.VITE_API_BASE_URL ?? "/";

const config = {
  baseUrl: baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`,
  googleClientId: import.meta.env.VITE_GOOGLE_CLIENT_ID ?? "",
} as const;

export default config;
