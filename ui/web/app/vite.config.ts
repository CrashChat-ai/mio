import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const devApiTarget = process.env.VITE_DEV_API_TARGET ?? "http://localhost:8081";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    sourcemap: true,
  },
  server: {
    proxy: {
      "/api": devApiTarget,
      "/auth": devApiTarget,
      "/healthz": devApiTarget,
    },
  },
});
