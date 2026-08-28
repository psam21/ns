import HttpErrors from "http-errors";
import { BlobMetadata } from "blossom-server-sdk";
import dayjs from "dayjs";
import { koaBody } from "koa-body";
import { IncomingMessage } from "node:http";
import * as http from "node:http";
import * as https from "node:https";
import { lookup as dnsLookup } from "node:dns/promises";
import net from "node:net";
import mount from "koa-mount";

import storage from "../storage/index.js";
import { CommonState, getBlobDescriptor, log, router } from "./router.js";
import { getFileRule } from "../rules/index.js";
import { config } from "../config.js";
import { updateBlobAccess } from "../db/methods.js";
import { UploadDetails, readUpload, removeUpload, saveFromResponse } from "../storage/upload.js";
import { blobDB } from "../db/db.js";

const MAX_REDIRECTS = 3;
const MIRROR_TIMEOUT_MS = 15_000;

function isBlockedAddress(address: string): boolean {
  const normalized = address.toLowerCase();
  const mappedIPv4 = normalized.match(/^::ffff:(\d+(?:\.\d+){3})$/)?.[1];
  if (mappedIPv4) return isBlockedAddress(mappedIPv4);

  if (net.isIPv4(normalized)) {
    const octets = normalized.split(".").map(Number);
    const [first, second] = octets;
    return (
      first === 0 ||
      first === 10 ||
      first === 127 ||
      (first === 100 && second >= 64 && second <= 127) ||
      (first === 169 && second === 254) ||
      (first === 172 && second >= 16 && second <= 31) ||
      (first === 192 && second === 0) ||
      (first === 192 && second === 168) ||
      (first === 198 && (second === 18 || second === 19)) ||
      first >= 224
    );
  }

  if (net.isIPv6(normalized)) {
    return (
      normalized === "::" ||
      normalized === "::1" ||
      normalized.startsWith("fc") ||
      normalized.startsWith("fd") ||
      normalized.startsWith("fe8") ||
      normalized.startsWith("fe9") ||
      normalized.startsWith("fea") ||
      normalized.startsWith("feb")
    );
  }

  return true;
}

async function resolvePublicAddresses(hostname: string) {
  const lowerHostname = hostname.toLowerCase().replace(/\.$/, "");
  if (
    lowerHostname === "localhost" ||
    lowerHostname.endsWith(".localhost") ||
    lowerHostname.endsWith(".local") ||
    lowerHostname.endsWith(".internal")
  ) {
    throw new HttpErrors.BadRequest("SSRF blocked: restricted hostname");
  }

  const addresses = await dnsLookup(hostname, { all: true, verbatim: true });
  if (!addresses.length || addresses.some(({ address }) => isBlockedAddress(address))) {
    throw new HttpErrors.BadRequest("SSRF blocked: private or restricted address");
  }
  return addresses;
}

async function makeRequestWithAbort(
  url: URL,
  redirectCount = 0,
  cancelController = new AbortController(),
): Promise<{ response: IncomingMessage; controller: AbortController }> {
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new HttpErrors.BadRequest("Only HTTP and HTTPS mirror URLs are supported");
  }
  if (url.username || url.password) throw new HttpErrors.BadRequest("Mirror URLs cannot contain credentials");
  if (url.port && url.port !== "80" && url.port !== "443") {
    throw new HttpErrors.BadRequest("Mirror URL port is not allowed");
  }

  const addresses = await resolvePublicAddresses(url.hostname);
  const address = addresses[0];
  const client = url.protocol === "https:" ? https : http;

  return await new Promise((resolve, reject) => {
    const request = client.get(
      url,
      {
        signal: cancelController.signal,
        timeout: MIRROR_TIMEOUT_MS,
        lookup: (_hostname, _options, callback) => callback(null, address.address, address.family),
      },
      (response) => {
        const status = response.statusCode ?? 0;
        const location = response.headers.location;
        if (status >= 300 && status < 400 && location) {
          if (redirectCount >= MAX_REDIRECTS) {
            response.resume();
            reject(new HttpErrors.BadRequest("Too many mirror redirects"));
            return;
          }
          response.resume();
          makeRequestWithAbort(new URL(location, url), redirectCount + 1, cancelController)
            .then(resolve)
            .catch(reject);
          return;
        }
        resolve({ response, controller: cancelController });
      },
    );
    request.on("timeout", () => request.destroy(new Error("Mirror request timed out")));
    request.on("error", reject);
  });
}

router.use(mount("/mirror", koaBody()));

router.put<CommonState>("/mirror", async (ctx) => {
  if (!config.upload.enabled) throw new HttpErrors.NotFound("Uploads disabled");

  // check auth
  if (config.upload.requireAuth) {
    if (!ctx.state.auth) throw new HttpErrors.Unauthorized("Missing Auth event");
    if (ctx.state.authType !== "upload") throw new HttpErrors.Unauthorized("Auth event should be 'upload'");
  }

  if (!ctx.request.body || typeof ctx.request.body !== "object" || !("url" in ctx.request.body)) {
    throw new HttpErrors.BadRequest("Missing url");
  }
  const body = ctx.request.body as { url?: unknown };
  if (typeof body.url !== "string" || body.url.length > 2048) {
    throw new HttpErrors.BadRequest("Invalid mirror URL");
  }

  let downloadUrl: URL;
  try {
    downloadUrl = new URL(body.url);
  } catch {
    throw new HttpErrors.BadRequest("Invalid mirror URL");
  }

  await resolvePublicAddresses(downloadUrl.hostname);
  log(`Mirroring ${downloadUrl.toString()}`);

  const { response, controller } = await makeRequestWithAbort(downloadUrl);
  let maybeUpload: UploadDetails | undefined = undefined;

  try {
    if (!response.statusCode) throw new HttpErrors.InternalServerError("Failed to make request");
    if (response.statusCode < 200 || response.statusCode >= 400)
      throw new HttpErrors.InternalServerError("Download request failed");

    // check rules
    const contentType = response.headers["content-type"];
    const pubkey = ctx.state.auth?.pubkey;

    const rule = getFileRule(
      {
        type: contentType,
        pubkey,
      },
      config.storage.rules,
      config.upload.requireAuth && config.upload.requirePubkeyInRule,
    );
    if (!rule) {
      if (config.upload.requirePubkeyInRule) throw new HttpErrors.Unauthorized("Pubkey not on whitelist");
      else throw new HttpErrors.Unauthorized(`Server dose not accept ${contentType} blobs`);
    }

    const upload = (maybeUpload = await saveFromResponse(response));
    const type = contentType || upload.type;

    if (config.upload.requireAuth) {
      // check if auth has blob sha256 hash
      if (!ctx.state.auth?.tags.some((t) => t[0] === "x" && t[1] === upload.sha256))
        throw new HttpErrors.BadRequest("Auth missing blob sha256 hash");
    }

    let blob: BlobMetadata;

    if (!blobDB.hasBlob(upload.sha256)) {
      log("Saving", upload.sha256, type);
      await storage.writeBlob(upload.sha256, readUpload(upload), type);
      await removeUpload(upload);

      const now = dayjs().unix();
      blob = blobDB.addBlob({ sha256: upload.sha256, size: upload.size, type, uploaded: now });
      updateBlobAccess(upload.sha256, dayjs().unix());
    } else {
      blob = blobDB.getBlob(upload.sha256);
      await removeUpload(upload);
    }

    if (pubkey && !blobDB.hasOwner(upload.sha256, pubkey)) {
      blobDB.addOwner(blob.sha256, pubkey);
    }

    ctx.status = 200;
    ctx.body = getBlobDescriptor(blob, ctx.request);
  } catch (error) {
    // cancel the request if anything fails
    controller.abort();
    if (maybeUpload) await removeUpload(maybeUpload);

    throw error;
  }
});
