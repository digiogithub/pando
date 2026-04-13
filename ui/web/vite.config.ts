import { defineConfig } from "vite";
import solid from "vite-plugin-solid";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

export default defineConfig({
  plugins: [solid(), tailwindcss()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "./src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 3000,
    proxy: {
      "/api": {
        target: "http://localhost:8765",
        changeOrigin: true,
      },
      "/health": {
        target: "http://localhost:8765",
        changeOrigin: true,
      },
    },
  },
  build: {
    target: "esnext",
    outDir: "dist",
    sourcemap: true,
  },
  appType: "spa",
});
