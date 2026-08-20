import type { Agent, IncomingMessage } from "http";
import { SocksProxyAgent } from "socks-proxy-agent";
import followRedirects from "follow-redirects";
const { http, https } = followRedirects;

import { HTTPPointer } from "../types.js";
import { config } from "../config.js";

// Block private/restricted IP ranges to prevent SSRF (finding #1)
function isPrivateIP(hostname: string): boolean {
  // Check for private/localhost/link-local ranges before resolving
  const privatePatterns = [
    /^127\./,
    /^10\./,
    /^192\.168\./,
    /^172\.(1[6-9]|2[0-9]|3[01])\./,
    /^169\.254\./,
    /^::1$/,
    /^fe80::/,
    /^fc00::/,
    /^::$/,
  ];
  return privatePatterns.some((p) => p.test(hostname));
}

export async function readHTTPPointer(pointer: HTTPPointer): Promise<IncomingMessage> {
  return new Promise((resolve, reject) => {
    const url = new URL(pointer.url);
    if (isPrivateIP(url.hostname)) {
      throw new Error(`SSRF blocked: cannot connect to private/restricted address: ${url.hostname}`);
    }
    let agent: Agent | undefined = undefined;

    if (url.hostname.endsWith(".onion")) {
      if (!config.tor.enabled) throw new Error("Cant load .onion address without tor");

      agent = new SocksProxyAgent(config.tor.proxy);
    }

    const backend = pointer.url.startsWith("https") ? https : http;

    backend
      .get(pointer.url, { agent }, (res) => {
        res.once("error", (error) => reject(error));

        if (!res.statusCode) return reject();
        if (res.statusCode < 200 || res.statusCode >= 400) {
          res.destroy();
          reject(res);
        } else resolve(res);
      })
      .on("error", (err) => {
        reject(err);
      })
      .end();
  });
}
