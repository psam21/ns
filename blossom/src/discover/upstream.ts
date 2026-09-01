import * as follow from "follow-redirects";
const { http, https } = follow;

import { BlobSearch, HTTPPointer } from "../types.js";
import { config } from "../config.js";
import logger from "../logger.js";
import { isBlockedAddress, resolvePublicAddresses } from "../helpers/ssrf.js";

const log = logger.extend("upstream-discovery");

export async function search(search: BlobSearch) {
  log("Looking for", search.hash + search.ext);
  for (const cdn of config.discovery.upstream.domains) {
    try {
      log("Checking", cdn);
      const pointer = await checkCDN(cdn, search);
      if (pointer) {
        log("Found", search.hash, "at", cdn);
        return pointer;
      }
    } catch (e) {
      log("CDN check failed", cdn, e instanceof Error ? e.message : String(e));
    }
  }
}

function checkCDN(cdn: string, search: BlobSearch): Promise<HTTPPointer> {
  return new Promise<HTTPPointer>((resolve, reject) => {
    let url: URL;
    try {
      url = new URL("/" + search.hash, cdn);
    } catch {
      return reject(new Error(`Invalid CDN URL: ${cdn}`));
    }

    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return reject(new Error(`CDN URL must be http(s): ${cdn}`));
    }
    if (url.username || url.password) {
      return reject(new Error(`CDN URL must not contain credentials: ${cdn}`));
    }
    if (url.port && url.port !== "80" && url.port !== "443") {
      return reject(new Error(`CDN URL must use default port: ${cdn}`));
    }

    // Defense in depth: even though CDNs are operator-configured, validate
    // the resolved IP to avoid accidental internal exposure if a future
    // config mistake points at a private hostname (issue #53).
    resolvePublicAddresses(url.hostname).catch((err) => reject(err));

    const backend = url.protocol === "https:" ? https : http;

    const request = backend.request(url.toString(), { method: "HEAD", timeout: 5 * 1000 }, () => {});

    request.on("response", (res) => {
      res.destroy();
      const contentLength = res.headers["content-length"];
      const length = contentLength ? parseInt(contentLength) : undefined;

      if (!res.statusCode) return reject(new Error("Missing status code"));
      if (!length) return reject(new Error("Missing Content-Length"));

      if (res.statusCode < 200 || res.statusCode >= 400) {
        reject(new Error("Not Found"));
      } else {
        resolve({
          kind: "http",
          url: url.toString(),
          hash: search.hash,
          size: length,
        });
      }
    });

    request.on("error", () => request.destroy());

    request.on("timeout", () => {
      request.destroy();
      reject(new Error("Timeout"));
    });

    request.end();
  });
}
