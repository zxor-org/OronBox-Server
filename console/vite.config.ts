import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "/admin/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
  },
  server: {
    proxy: {
      "/admin/api": "http://127.0.0.1:8080",
      "/admin/blobs": "http://127.0.0.1:8080",
      "/admin/logout": "http://127.0.0.1:8080",
      "/admin/login": "http://127.0.0.1:8080",
      "/oauth2": "http://127.0.0.1:8080",
    },
  },
});
