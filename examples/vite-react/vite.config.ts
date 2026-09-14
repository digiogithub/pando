import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// A plain Vite + React 18 client build, deliberately configured with no Node
// polyfills: this example proves `@pando-ai/sdk/agui/client` needs none, the
// same property `sdk/typescript/tests/browser-build` verifies for the SDK in
// isolation.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
  },
  build: {
    target: "es2022",
  },
});
