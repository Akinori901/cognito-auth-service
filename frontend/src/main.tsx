import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// Amplify.configure() を先に走らせる（副作用 import）。
import "@/auth/amplify-config";
import "@/styles.css";

import { App } from "@/App";

const qc = new QueryClient({
  defaultOptions: {
    queries: {
      // 認可の設定画面なので、古い値を見せない方が安全。
      staleTime: 0,
      retry: false,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);
