import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

/** @type {import('next').NextConfig} */
const nextConfig = {
  // This example lives inside the Pando repository, which has its own lockfile.
  // Without this, Next.js infers the repo root as the workspace root.
  outputFileTracingRoot: dirname(fileURLToPath(import.meta.url)),
  // The SDK ships ESM + CJS; nothing to transpile. Kept explicit so the example
  // still builds if the package is linked from source during development.
  transpilePackages: [],
};

export default nextConfig;
