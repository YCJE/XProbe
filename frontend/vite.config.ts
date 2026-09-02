import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// 构建产物输出到 frontend/dist, 由 make build-frontend / CI / Docker 拷贝到 server/web(Go embed 唯一源)。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "https://127.0.0.1:443",
      "/ws": { target: "ws://127.0.0.1:443", ws: true },
    },
  },
});
