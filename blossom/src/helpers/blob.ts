import { BlobMetadata } from "blossom-server-sdk";
import mime from "mime";
import { config } from "../config.js";

// Build a fully-qualified URL for a blob. If config.publicDomain is unset,
// REFUSE to fall back to the request's Host header, which can leak an
// internal Caddy hostname (issue #89).
export function getBlobURL(blob: Pick<BlobMetadata, "sha256" | "type">, host?: string) {
  const ext = blob.type && mime.getExtension(blob.type);
  if (config.publicDomain) {
    return new URL(blob.sha256 + (ext ? "." + ext : ""), config.publicDomain).toString();
  }
  if (!host) {
    throw new Error("publicDomain is not configured; cannot build blob URL");
  }
  // Explicit override (e.g. tests) — caller is responsible for the value.
  return new URL(blob.sha256 + (ext ? "." + ext : ""), host).toString();
}
