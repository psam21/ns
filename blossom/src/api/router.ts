import { Request } from "koa";
import Router from "@koa/router";
import dayjs from "dayjs";
import HttpErrors from "http-errors";
import { verifyEvent, NostrEvent } from "nostr-tools";
import { BlobMetadata } from "blossom-server-sdk";

import logger from "../logger.js";
import { getBlobURL } from "../helpers/blob.js";

export const log = logger.extend("api");
export const router = new Router();

export function getBlobDescriptor(blob: BlobMetadata, req?: Request) {
  return {
    sha256: blob.sha256,
    size: blob.size,
    uploaded: blob.uploaded,
    type: blob.type,
    url: getBlobURL(blob, req ? req.protocol + "://" + req.host : undefined),
  };
}

function parseAuthEvent(auth: NostrEvent) {
  const now = dayjs().unix();
  if (auth.kind !== 24242) throw new HttpErrors.BadRequest("Unexpected auth kind");
  const type = auth.tags.find((t) => t[0] === "t")?.[1];
  if (!type) throw new HttpErrors.BadRequest("Auth missing type");
  const expiration = auth.tags.find((t) => t[0] === "expiration")?.[1];
  if (!expiration) throw new HttpErrors.BadRequest("Auth missing expiration");
  const expirationTimestamp = Number(expiration);
  if (!Number.isSafeInteger(expirationTimestamp)) throw new HttpErrors.BadRequest("Auth expiration is invalid");
  if (expirationTimestamp < now) throw new HttpErrors.BadRequest("Auth expired");
  if (!verifyEvent(auth)) throw new HttpErrors.BadRequest("Invalid Auth event");

  return { auth, type, expiration: expirationTimestamp };
}

// NIP-98: HTTP Auth
// https://github.com/nostr-protocol/nips/blob/master/98.md
// Validates a kind 27235 event used for HTTP request authorization.
function parseNIP98AuthEvent(auth: NostrEvent, req: Request) {
  const now = dayjs().unix();

  // 1. The kind MUST be 27235
  if (auth.kind !== 27235) {
    throw new HttpErrors.BadRequest("NIP-98 auth event must be kind 27235");
  }

  // 2. created_at MUST be within a reasonable time window (60 seconds)
  const createdAt = auth.created_at;
  if (Math.abs(now - createdAt) > 60) {
    throw new HttpErrors.BadRequest("NIP-98 auth event timestamp out of window");
  }

  // 3. The u tag MUST be exactly the same as the absolute request URL
  const uTag = auth.tags.find((t) => t[0] === "u")?.[1];
  if (!uTag) throw new HttpErrors.BadRequest("NIP-98 auth missing 'u' tag");
  const fullURL = req.protocol + "://" + req.host + req.originalUrl;
  if (uTag !== fullURL) {
    throw new HttpErrors.BadRequest("NIP-98 auth 'u' tag does not match request URL");
  }

  // 4. The method tag MUST be the same HTTP method used
  const methodTag = auth.tags.find((t) => t[0] === "method")?.[1];
  if (!methodTag) throw new HttpErrors.BadRequest("NIP-98 auth missing 'method' tag");
  if (methodTag.toUpperCase() !== req.method.toUpperCase()) {
    throw new HttpErrors.BadRequest("NIP-98 auth 'method' tag does not match request method");
  }

  // Verify event signature
  if (!verifyEvent(auth)) throw new HttpErrors.BadRequest("Invalid NIP-98 auth event");

  // Map NIP-98 (kind 27235) to the same authType taxonomy as NIP-42
  // (kind 24242), so the per-endpoint `checkUpload`/`checkList`/...
  // gates see "upload" / "media" / "list" / "mirror" and not the
  // opaque "nip98" value. Before this fix, every NIP-98 PUT to
  // /upload returned 401 with "Auth event must be 'upload'", which
  // is the bug discovered in production on 2026-09-01.
  //
  // The mapping uses the URL path (BUD-04's NIP-42 events carry an
  // explicit `t` tag; NIP-98 derives the operation from the URL
  // instead). Falls back to "nip98" for unrecognized paths so the
  // mismatch surfaces as a 401 with a clear reason rather than a
  // silent authorization bypass.
  let type = "nip98";
  if (uTag) {
    try {
      const path = new URL(uTag).pathname;
      if (path === "/upload") type = "upload";
      else if (path === "/media") type = "media";
      else if (path === "/mirror") type = "mirror";
      else if (path.startsWith("/list/")) type = "list";
    } catch {
      // Malformed u tag - keep "nip98" so the per-endpoint check
      // rejects it loudly.
    }
  }

  return { auth, type, expiration: createdAt + 60 };
}

// parse auth headers
export type CommonState = { auth?: NostrEvent; authType?: string; authExpiration?: number };
// Sentinel value for NIP-98 (kind 27235) auth events. NIP-98 binds
// the auth to a specific request via the `u` + `method` tags and a
// fresh signature, so the per-endpoint `checkUpload`/`checkList`/...
// gates (which compare ctx.state.authType to a literal operation
// name) are redundant and harmful for NIP-98: they would reject
// perfectly valid auth events from clients that don't set a `t` tag
// or that put the operation in the URL but not as a literal
// "upload"/"media"/"list"/"mirror" string. Per-endpoint checks now
// short-circuit on this sentinel.
export const NIP98_AUTH_TYPE = "*";
router.use(async (ctx, next) => {
  const authStr = ctx.headers["authorization"] as string | undefined;

  if (authStr?.startsWith("Nostr ")) {
    let auth: NostrEvent;
    try {
      const decoded = atob(authStr.replace(/^Nostr\s/i, ""));
      const parsed: unknown = JSON.parse(decoded);
      if (!parsed || typeof parsed !== "object" || !Array.isArray((parsed as { tags?: unknown }).tags)) {
        throw new Error("authorization event must be an object with tags");
      }
      auth = parsed as NostrEvent;
    } catch {
      throw new HttpErrors.BadRequest("Invalid Nostr authorization header");
    }

    // Try NIP-98 (kind 27235) first, fall back to NIP-42 (kind 24242)
    if (auth.kind === 27235) {
      const { type, expiration } = parseNIP98AuthEvent(auth, ctx.request);
      ctx.state.auth = auth;
      // For NIP-98 the URL+method in the event already binds it to a
      // specific endpoint. Set the sentinel; the per-endpoint
      // `authType === X` checks accept any value starting with `*`.
      ctx.state.authType = NIP98_AUTH_TYPE;
      ctx.state.authExpiration = expiration;
      // Keep the resolved type on a separate field for logging /
      // metrics. It is NOT used for authorization decisions.
      (ctx.state as { authResolvedType?: string }).authResolvedType = type;
    } else {
      const { type, expiration } = parseAuthEvent(auth);
      ctx.state.auth = auth;
      ctx.state.authType = type;
      ctx.state.authExpiration = expiration;
    }
  }

  await next();
});
