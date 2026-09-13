import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const nodeModules = path.join(root, "node_modules");

async function resolvePackage(packageName) {
  const directPackagePath = path.join(nodeModules, packageName);
  try {
    return await fs.realpath(directPackagePath);
  } catch {
    const virtualStore = path.join(nodeModules, ".pnpm");
    const entries = await fs.readdir(virtualStore);
    const packageEntry = entries.find((entry) => entry.startsWith(`${packageName}@`));
    if (!packageEntry) return null;
    return path.join(virtualStore, packageEntry, "node_modules", packageName);
  }
}

const streamJson = await resolvePackage("stream-json");
const minio = await resolvePackage("minio");
if (!streamJson || !minio) {
  process.exit(0);
}

const lowercaseParser = path.join(streamJson, "src", "jsonl", "parser.js");
const uppercaseParser = path.join(streamJson, "src", "jsonl", "Parser.js");
try {
  await fs.access(uppercaseParser);
} catch {
  await fs.copyFile(lowercaseParser, uppercaseParser);
}

for (const relativePath of ["dist/esm/notification.mjs", "dist/main/notification.js"]) {
  const notificationPath = path.join(minio, relativePath);
  try {
    const source = await fs.readFile(notificationPath, "utf8");
    const normalized = source.replaceAll("stream-json/jsonl/Parser.js", "stream-json/jsonl/parser.js");
    if (normalized !== source) {
      await fs.writeFile(notificationPath, normalized);
    }
  } catch {
    // The production dependency set may not include both module formats.
  }
}
