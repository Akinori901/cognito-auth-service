import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    port: 5173,
    // バックエンドはコンテナの 8080。/api を素通しにして
    // CORS 設定を持たずに済ませる（本番は CloudFront が同じ役割をする）。
    proxy: {
      "/api": { target: "http://backend:8080", changeOrigin: true },
    },
  },
});
