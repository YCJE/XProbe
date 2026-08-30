import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// 前端构建产物输出到 server/web/(Go embed 唯一源, 设计文档 3.3)。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../server/web",
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
