import HttpErrors from "http-errors";
import { BlobMetadata } from "blossom-server-sdk";
import dayjs from "dayjs";
import { koaBody } from "koa-body";
import { IncomingMessage } from "node:http";
import * as http from "node:http";
import * as https from "node:https";
import mount from "koa-mount";

import storage from "../storage/index.js";
import { CommonState, getBlobDescriptor, log, NIP98_AUTH_TYPE, router } from "./router.js";
import { getFileRule } from "../rules/index.js";
import { config } from "../config.js";
import { updateBlobAccess } from "../db/methods.js";
import { UploadDetails, readUpload, removeUpload, saveFromResponse } from "../storage/upload.js";
import { blobDB } from "../db/db.js";
import { resolvePublicAddresses } from "../helpers/ssrf.js";

const MAX_REDIRECTS = 3;
const MIRROR_TIMEOUT_MS = 15_000;

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

  // Validate EVERY resolved address (issue #81) and pin a single one
  // for this connection. Cross-host redirects re-run this check.
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
          // Re-resolve and re-validate the redirect target's hostname;
          // do not re-use the original pinned IP (issue #82).
          let nextUrl: URL;
          try {
            nextUrl = new URL(location, url);
          } catch {
            reject(new HttpErrors.BadRequest("Invalid mirror redirect target"));
            return;
          }
          if (nextUrl.hostname !== url.hostname) {
            // Different host: force a fresh SSRF re-check.
            resolvePublicAddresses(nextUrl.hostname).catch((err) => {
              response.destroy();
              reject(err instanceof Error ? new HttpErrors.BadRequest(err.message) : err);
            });
          }
          makeRequestWithAbort(nextUrl, redirectCount + 1, cancelController)
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
    // NIP-98 binds via `u` + `method` tags; per-endpoint `t` tag
    // check is redundant for NIP-98 clients.
    if (ctx.state.authType !== NIP98_AUTH_TYPE && ctx.state.authType !== "upload") {
      throw new HttpErrors.Unauthorized("Auth event should be 'upload'");
    }
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
