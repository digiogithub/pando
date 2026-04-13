import { brotliCompressSync, constants, gzipSync } from "node:zlib";
import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";

const rootDir = resolve(process.cwd());
const distDir = join(rootDir, "dist");
const outDir = resolve(rootDir, "../../internal/api/webui/dist");
const compressibleExtensions = new Set([".html", ".js", ".css", ".svg", ".json", ".txt", ".map"]);

if (!existsSync(distDir)) {
  throw new Error(`dist directory not found: ${distDir}`);
}

rmSync(outDir, { recursive: true, force: true });
mkdirSync(outDir, { recursive: true });
cpSync(distDir, outDir, { recursive: true });

const walk = (dir) => {
  for (const entry of readdirSync(dir)) {
    const fullPath = join(dir, entry);
    const stats = statSync(fullPath);
    if (stats.isDirectory()) {
      walk(fullPath);
      continue;
    }

    const relPath = relative(outDir, fullPath);
    const ext = relPath.slice(relPath.lastIndexOf("."));
    if (!compressibleExtensions.has(ext)) {
      continue;
    }

    const input = readFileSync(fullPath);
    writeFileSync(`${fullPath}.gz`, gzipSync(input, { level: 9 }));
    writeFileSync(
      `${fullPath}.br`,
      brotliCompressSync(input, {
        params: {
          [constants.BROTLI_PARAM_QUALITY]: 11,
        },
      })
    );
  }
};

walk(outDir);
console.log(`Embedded UI assets prepared in ${outDir}`);
