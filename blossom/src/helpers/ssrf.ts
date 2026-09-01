// Shared SSRF defense helpers, used by both mirror.ts and discover/upstream.ts.
// Originally inlined in mirror.ts; lifted to a single module so a future
// feature cannot accidentally skip the guard (issue #53, #58, #81).

import * as net from "node:net";
import { lookup as dnsLookup } from "node:dns/promises";

const blockedHostnames = new Set([
  "localhost",
  "ip6-localhost",
  "ip6-loopback",
]);

function endsWithAny(hostname: string, suffixes: string[]): boolean {
  const lower = hostname.toLowerCase();
  return suffixes.some((s) => lower.endsWith(s));
}

function isBlockedHostname(hostname: string): boolean {
  if (blockedHostnames.has(hostname)) return true;
  if (endsWithAny(hostname, [".localhost", ".local", ".internal", ".test", ".example"])) {
    return true;
  }
  return false;
}

function isBlockedIPv4(address: string): boolean {
  if (!net.isIPv4(address)) return false;
  const octets = address.split(".").map(Number);
  const [first, second] = octets;
  if (first === undefined) return true;
  if (first === 0 || first === 10 || first === 127) return true;
  if (first === 100 && second !== undefined && second >= 64 && second <= 127) return true; // CGNAT
  if (first === 169 && second === 254) return true; // link-local
  if (first === 172 && second !== undefined && second >= 16 && second <= 31) return true; // private
  if (first === 192 && second === 168) return true; // private
  if (first === 192 && second === 0) return true; // IETF
  if (first === 198 && (second === 18 || second === 19)) return true; // benchmark
  if (first === 198 && second === 51 && octets[2] === 100) return true; // TEST-NET-2
  if (first === 203 && second === 0 && octets[2] === 113) return true; // TEST-NET-3
  if (first >= 224) return true; // multicast / reserved
  return false;
}

function isBlockedIPv6(address: string): boolean {
  if (!net.isIPv6(address)) return false;
  const lower = address.toLowerCase();
  if (lower === "::" || lower === "::1") return true;
  if (lower.startsWith("fc") || lower.startsWith("fd")) return true; // ULA
  if (
    lower.startsWith("fe8") ||
    lower.startsWith("fe9") ||
    lower.startsWith("fea") ||
    lower.startsWith("feb")
  ) {
    return true; // link-local
  }
  if (lower.startsWith("ff")) return true; // multicast
  // IPv4-mapped IPv6 (::ffff:1.2.3.4)
  const mapped = lower.match(/^::ffff:(\d+(?:\.\d+){3})$/);
  if (mapped) return isBlockedIPv4(mapped[1]);
  return false;
}

export function isBlockedAddress(address: string): boolean {
  const normalized = address.toLowerCase();
  return isBlockedIPv4(normalized) || isBlockedIPv6(normalized);
}

export interface ResolvedAddress {
  address: string;
  family: 4 | 6;
}

/**
 * Resolve the given hostname and reject if any resolved address is in a
 * blocked range. Returns every address so callers can pin the one they
 * will connect to (and re-validate on redirect).
 */
export async function resolvePublicAddresses(hostname: string): Promise<ResolvedAddress[]> {
  const lowerHostname = hostname.toLowerCase().replace(/\.$/, "");
  if (isBlockedHostname(lowerHostname)) {
    throw new Error(`SSRF blocked: restricted hostname ${hostname}`);
  }
  if (net.isIP(lowerHostname)) {
    // A literal IP — reject if it is itself blocked.
    if (isBlockedAddress(lowerHostname)) {
      throw new Error(`SSRF blocked: private or restricted address ${hostname}`);
    }
    return [{ address: lowerHostname, family: net.isIPv4(lowerHostname) ? 4 : 6 }];
  }
  let addresses: { address: string; family: number }[];
  try {
    addresses = await dnsLookup(lowerHostname, { all: true, verbatim: true });
  } catch (err) {
    throw new Error(`SSRF blocked: DNS resolution failed for ${hostname}: ${(err as Error).message}`);
  }
  if (!addresses.length || addresses.some((a) => isBlockedAddress(a.address))) {
    throw new Error(`SSRF blocked: private or restricted address for ${hostname}`);
  }
  return addresses.map((a) => ({ address: a.address, family: a.family === 6 ? 6 : 4 }));
}
