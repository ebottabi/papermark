import { BrowserRouter } from "react-router-dom";
import { QueryClientProvider } from "@tanstack/react-query";
import { GoogleOAuthProvider } from "@react-oauth/google";
import { queryClient } from "@/lib/query-client";
import config from "@/lib/config";
import { AuthProvider } from "@/context/auth-context";
import { TooltipProvider, ToastViewport } from "@/components/ui";
import { AppRoutes } from "@/routes/routes";

const App = () => (
  <GoogleOAuthProvider clientId={config.googleClientId}>
    <BrowserRouter>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <TooltipProvider delayDuration={200}>
            <AppRoutes />
            <ToastViewport />
          </TooltipProvider>
        </AuthProvider>
      </QueryClientProvider>
    </BrowserRouter>
  </GoogleOAuthProvider>
);

export default App;
