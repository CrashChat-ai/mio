import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    sourcemap: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8081",
      "/auth": "http://localhost:8081",
      "/healthz": "http://localhost:8081",
    },
  },
});
