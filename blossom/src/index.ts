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

// CORS: restrict to a configured allow-list (defaults to publicDomain).
// Wildcard origin + exposed Authorization header is a cross-origin
// reconnaissance / CSRF surface (issue #54).
const corsAllowOrigins = (config.publicDomain ? [new URL(config.publicDomain).origin] : []).concat(
  (process.env.BLOSSOM_EXTRA_CORS_ORIGINS || "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean),
);
app.use(
  cors({
    origin: (ctx) => {
      const origin = ctx.request.header.origin;
      if (!origin) return "";
      return corsAllowOrigins.includes(origin) ? origin : corsAllowOrigins[0] || "";
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

// handle errors
app.use(async (ctx, next) => {
  try {
    await next();
  } catch (err) {
    if (isHttpError(err)) {
      const status = (ctx.status = err.status || 500);
      if (status >= 500) console.error(err.stack);
      ctx.set("X-Reason", publicReason(err, status));
    } else {
      console.log(err);
      ctx.status = 500;
      ctx.set("X-Reason", publicReason(err, 500));
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
