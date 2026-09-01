#!/usr/bin/env node
import "./polyfill.js";
import Koa from "koa";
import serve from "koa-static";
import path from "node:path";
import cors from "@koa/cors";
import mount from "koa-mount";
import fs from "node:fs";
import { fileURLToPath } from "node:url";

import router from "./api/index.js";
import logger from "./logger.js";
import { config } from "./config.js";
import { isHttpError } from "./helpers/error.js";
import db from "./db/db.js";
import { pruneStorage } from "./storage/index.js";
import { generate } from "generate-password";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const app = new Koa();

// Apply baseline browser security headers to API, dashboard, and static responses.
app.use(async (ctx, next) => {
  ctx.set("X-Content-Type-Options", "nosniff");
  ctx.set("X-Frame-Options", "DENY");
  ctx.set("Referrer-Policy", "no-referrer");
  ctx.set("Permissions-Policy", "camera=(), microphone=(), geolocation=()");
  await next();
});

// trust reverse proxy headers
app.proxy = true;

// CORS model.
//
// Security on this server is provided by NIP-42 / NIP-98 auth
// (token-based, with a fresh signature and a 60-second window), not
// by CORS origin checking. CORS is a browser-side gate that
// determines whether the JS code can *read* a response; the server
// still processes the request either way. Restricting CORS to an
// origin allow-list therefore adds no real security and breaks
// every browser client (chattr.buzz, custom web UIs, dev pages on
// localhost, ...) that isn't in the list. Real-world public Blossom
// servers (nostr.build, primal, blossom.band) all serve with
// wildcard CORS for the same reason.
//
// Defaults:
//   - If the operator sets `extraCorsOrigins` in config.yml or
//     BLOSSOM_EXTRA_CORS_ORIGINS in the environment, the server
//     uses an allow-list (only those origins + publicDomain get
//     ACAO). This is the explicit opt-in for restrictive mode.
//   - Otherwise, the server uses a wildcard `*` for all methods.
//     Credentials are never sent (the NIP-98/NIP-42 Authorization
//     header is a Nostr event token, not a browser cookie), so
//     wildcard ACAO without credentials is the spec-safe choice.
//
// `publicDomain` alone does NOT trigger restrictive mode -- it's
// the canonical URL for blob descriptors, not a CORS gate. Only
// `extraCorsOrigins` (or the env override) is opt-in for
// restrictive CORS.
//
// Configure with:
//   config.publicDomain            -> used as the URL prefix in
//                                     blob descriptors; ignored
//                                     for CORS
//   config.extraCorsOrigins[]      -> opt-in to restrictive CORS
//                                     (allowed origins get ACAO;
//                                     others get nothing)
//   BLOSSOM_EXTRA_CORS_ORIGINS     -> same, env override
const explicitExtraOrigins = [
  ...((Array.isArray(config.extraCorsOrigins) ? config.extraCorsOrigins : []).map((s) => s.trim()).filter(Boolean)),
  ...(process.env.BLOSSOM_EXTRA_CORS_ORIGINS || "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean),
];
const restrictiveMode = explicitExtraOrigins.length > 0;
const explicitAllowList = restrictiveMode
  ? [
      ...(config.publicDomain ? [new URL(config.publicDomain).origin] : []),
      ...explicitExtraOrigins,
    ]
  : [];
if (restrictiveMode) {
  console.info(
    `[blossom] CORS: restrictive allow-list mode. Allowed: ${explicitAllowList.join(", ")}. ` +
      `Browser clients on other origins will be rejected with a CORS error. ` +
      `To accept uploads from any browser, leave extraCorsOrigins and ` +
      `BLOSSOM_EXTRA_CORS_ORIGINS empty.`,
  );
} else {
  console.info(
    "[blossom] CORS: wildcard mode (Access-Control-Allow-Origin: *). " +
      "All browser clients can call any endpoint. This is the recommended " +
      "default because security is provided by NIP-42 / NIP-98 auth, not " +
      "by origin checking. Set config.extraCorsOrigins to switch to " +
      "restrictive allow-list mode.",
  );
}
app.use(
  cors({
    origin: (ctx) => {
      const origin = ctx.request.header.origin;
      if (!origin) return "";
      // Wildcard mode: any browser origin can call any endpoint. The
      // NIP-42 / NIP-98 Authorization header is the actual auth; CORS
      // origin is not used as a security control.
      if (!restrictiveMode) return "*";
      // Restrictive mode: echo the origin back only when it is in
      // the allow-list; otherwise return "" so @koa/cors omits ACAO
      // and the browser refuses the response.
      return explicitAllowList.includes(origin) ? origin : "";
    },
    allowMethods: "GET,HEAD,PUT,POST,DELETE,OPTIONS",
    allowHeaders: "Authorization,Content-Type,X-Sha-256,X-Content-Type,X-Content-Length",
    exposeHeaders: "X-Reason,Content-Range,Content-Length",
    credentials: false,
    maxAge: 86400,
  }),
);

// Map known HttpError codes to short, stable reasons. The detail of an
// underlying error is rarely useful to a legitimate client and routinely
// leaks SSRF / auth / parser internals (issue #55).
function publicReason(err: unknown, status: number): string {
  if (status >= 500) return "internal_error";
  // Prefer the error's exposed "status" code to the message; the message
  // is sanitized in release mode.
  if (process.env.BLOSSOM_DEBUG_REASONS === "true" && err instanceof Error) {
    return err.message;
  }
  if (status === 400) return "bad_request";
  if (status === 401) return "unauthorized";
  if (status === 403) return "forbidden";
  if (status === 404) return "not_found";
  if (status === 405) return "method_not_allowed";
  if (status === 409) return "conflict";
  if (status === 413) return "payload_too_large";
  if (status === 416) return "range_not_satisfiable";
  if (status === 422) return "unprocessable_entity";
  if (status === 429) return "too_many_requests";
  if (status >= 400 && status < 500) return "client_error";
  return "error";
}

// Dedicated 5xx stack-trace sink. Writing full stacks to stdout leaks
// file paths and internal details into the systemd journal (issue #89).
// Stacks go to a dedicated file under the data directory; stdout is
// reserved for clean operational lines.
const errorsLogPath = path.join(path.dirname(config.databasePath), "errors.log");
let errorsLogStream: fs.WriteStream | null = null;
function getErrorsLogStream(): fs.WriteStream {
  if (errorsLogStream) return errorsLogStream;
  try {
    fs.mkdirSync(path.dirname(errorsLogPath), { recursive: true });
    errorsLogStream = fs.createWriteStream(errorsLogPath, { flags: "a", mode: 0o600 });
    errorsLogStream.on("error", () => {
      // Don't crash the process if the error log is unwritable.
      errorsLogStream = null;
    });
  } catch {
    errorsLogStream = null;
  }
  return errorsLogStream as unknown as fs.WriteStream;
}

// handle errors
app.use(async (ctx, next) => {
  try {
    await next();
  } catch (err) {
    if (isHttpError(err)) {
      const status = (ctx.status = err.status || 500);
      if (status >= 500) {
        const stream = getErrorsLogStream();
        const line = `[${new Date().toISOString()}] ${ctx.method} ${ctx.path}\n${err.stack || String(err)}\n`;
        if (stream) stream.write(line);
        else console.error(line);
      }
      ctx.set("X-Reason", publicReason(err, status));
    } else {
      const stream = getErrorsLogStream();
      const line = `[${new Date().toISOString()}] ${ctx.method} ${ctx.path}\n${err instanceof Error ? err.stack : String(err)}\n`;
      if (stream) stream.write(line);
      else console.log(line);
      ctx.status = 500;
      ctx.set("X-Reason", publicReason(err, 500));
    }
  }
});

// Add a strict CSP for the static upload UI (issue #90). The API routes
// already use APISecurityHeaders' CSP (default-src 'none').
app.use(async (ctx, next) => {
  await next();
  // Only set on HTML responses to avoid breaking JSON API clients.
  const ct = ctx.response.get("Content-Type") || "";
  if (ct.startsWith("text/html")) {
    if (!ctx.response.get("Content-Security-Policy")) {
      ctx.set(
        "Content-Security-Policy",
        "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'",
      );
    }
  }
});

app.use(router.routes()).use(router.allowedMethods());

if (config.dashboard.enabled) {
  const { koaBody } = await import("koa-body");
  const { default: basicAuth } = await import("koa-basic-auth");
  const { default: adminApi } = await import("./admin-api/index.js");

  let password = config.dashboard.password;
  if (!password) {
    // Generating a random password survives restarts only if persisted.
    // Refuse to start unless BLOSSOM_ALLOW_GENERATED_PASSWORD=1; in that
    // case write the password to a 0600 file next to the database and
    // print only the file path (issue #56).
    if (process.env.BLOSSOM_ALLOW_GENERATED_PASSWORD !== "1") {
      console.error(
        "FATAL: config.dashboard.password is empty and BLOSSOM_ALLOW_GENERATED_PASSWORD is not set.\n" +
          "Set dashboard.password in config.yml or export BLOSSOM_ADMIN_PASSWORD.",
      );
      process.exit(1);
    }
    password = generate();
    try {
      const fsSync = await import("node:fs");
      const pathMod = await import("node:path");
      const passwordFile = pathMod.join(pathMod.dirname(config.databasePath), ".blossom_admin_password");
      fsSync.writeFileSync(passwordFile, password, { mode: 0o600 });
      console.error(`Generated admin password written to ${passwordFile} (mode 0600). Do not commit.`);
    } catch (err) {
      console.error("FATAL: could not persist generated admin password:", err);
      process.exit(1);
    }
  }
  app.use(mount("/api", basicAuth({ name: config.dashboard.username, pass: password })));
  // Cap the body parser to keep unauthenticated memory pressure low
  // (issue #88). Basic-auth runs first; this limits the worst case.
  app.use(mount("/api", koaBody({ jsonLimit: "1mb", textLimit: "1mb", formLimit: "1mb" })));
  app.use(mount("/api", adminApi.routes())).use(mount("/api", adminApi.allowedMethods()));
  app.use(mount("/admin", serve(path.resolve(__dirname, "../admin/dist"))));

  // never log the password itself
  logger(`Dashboard started with username=${config.dashboard.username} (password length: ${password.length})`);
}

try {
  const www = path.resolve(process.cwd(), "public");
  fs.statSync(www);
  app.use(serve(www));
} catch (error) {
  const www = path.resolve(__dirname, "../public");
  app.use(serve(www));
}

app.listen(process.env.PORT || 3000);
logger("Started app on port", process.env.PORT || 3000);

async function cron() {
  try {
    await pruneStorage();
  } catch (error) {}
  setTimeout(cron, 30_000);
}

setTimeout(cron, 60_000);

async function shutdown() {
  logger("Saving database...");
  db.close();
  process.exit(0);
}

process.addListener("SIGTERM", shutdown);
process.addListener("SIGINT", shutdown);
