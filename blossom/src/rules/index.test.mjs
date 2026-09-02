// Unit tests for getFileRule empty-list behavior.
// Run with: pnpm test
// Imports from the compiled build/ output to avoid TypeScript runtime issues.
import { test } from "node:test";
import assert from "node:assert/strict";
import { getFileRule } from "../../build/rules/index.js";

const VALID_PUBKEY = "a".repeat(64);

test("getFileRule returns null when ruleset is empty", () => {
  assert.strictEqual(getFileRule({ type: "image/png" }, [], false), null);
});

test("getFileRule returns null when ruleset is empty even with a pubkey", () => {
  assert.strictEqual(
    getFileRule({ type: "image/png", pubkey: VALID_PUBKEY }, [], false),
    null,
  );
});

test("getFileRule returns null when ruleset is empty and requirePubkey is true", () => {
  assert.strictEqual(
    getFileRule({ type: "image/png", pubkey: VALID_PUBKEY }, [], true),
    null,
  );
});

test("getFileRule returns matching rule when ruleset has a type match", () => {
  const rule = { id: "test", type: "image/png", expiration: "1 day" };
  assert.strictEqual(getFileRule({ type: "image/png" }, [rule], false), rule);
});

test("getFileRule returns null when no rule matches the content type", () => {
  const rule = { id: "test", type: "image/png", expiration: "1 day" };
  assert.strictEqual(getFileRule({ type: "application/pdf" }, [rule], false), null);
});

test("getFileRule matches wildcard type '*' against any content type", () => {
  const wildcard = { id: "any", type: "*", expiration: "1 day" };
  assert.strictEqual(
    getFileRule({ type: "application/octet-stream" }, [wildcard], false),
    wildcard,
  );
});

test("getFileRule matches prefix wildcard 'image/*' against image subtypes", () => {
  const imageWildcard = {
    id: "images",
    type: "image/*",
    expiration: "1 day",
  };
  assert.strictEqual(
    getFileRule({ type: "image/jpeg" }, [imageWildcard], false),
    imageWildcard,
  );
});

test("getFileRule rejects when requirePubkey is true and rule has no pubkey list", () => {
  const rule = { id: "test", type: "image/png", expiration: "1 day" };
  assert.strictEqual(
    getFileRule({ type: "image/png", pubkey: VALID_PUBKEY }, [rule], true),
    null,
  );
});

test("getFileRule accepts when pubkey is in the rule's pubkey list", () => {
  const restricted = {
    id: "restricted",
    type: "*",
    expiration: "1 day",
    pubkeys: [VALID_PUBKEY],
  };
  assert.strictEqual(
    getFileRule({ type: "image/png", pubkey: VALID_PUBKEY }, [restricted], true),
    restricted,
  );
});

// Production rule set from blossom/config.yml.
// Keep these in sync with the rules in config.yml and deploy/blossom-rules-defaults.sh.
const PRODUCTION_RULES = [
  { id: "text", type: "text/*", expiration: "1 month" },
  { id: "image", type: "image/*", expiration: "1 month" },
  { id: "video", type: "video/*", expiration: "1 month" },
  { id: "audio", type: "audio/*", expiration: "1 month" },
  { id: "model", type: "model/*", expiration: "1 month" },
  { id: "catchall", type: "*", expiration: "2 days" },
];

test("production rules accept text/plain", () => {
  assert.ok(getFileRule({ type: "text/plain" }, PRODUCTION_RULES, false));
});

test("production rules accept image/png", () => {
  assert.ok(getFileRule({ type: "image/png" }, PRODUCTION_RULES, false));
});

test("production rules accept image/jpeg", () => {
  assert.ok(getFileRule({ type: "image/jpeg" }, PRODUCTION_RULES, false));
});

test("production rules accept video/mp4", () => {
  assert.ok(getFileRule({ type: "video/mp4" }, PRODUCTION_RULES, false));
});

test("production rules accept audio/mpeg", () => {
  assert.ok(getFileRule({ type: "audio/mpeg" }, PRODUCTION_RULES, false));
});

test("production rules accept model/gltf-binary via prefix wildcard", () => {
  assert.ok(getFileRule({ type: "model/gltf-binary" }, PRODUCTION_RULES, false));
});

test("production rules accept application/octet-stream via catchall '*'", () => {
  const rule = getFileRule(
    { type: "application/octet-stream" },
    PRODUCTION_RULES,
    false,
  );
  assert.ok(rule);
  assert.strictEqual(rule.id, "catchall");
});

test("production rules accept text/markdown", () => {
  const rule = getFileRule({ type: "text/markdown" }, PRODUCTION_RULES, false);
  assert.ok(rule);
  assert.strictEqual(rule.id, "text");
});

test("production rules accept font/woff2 via catchall", () => {
  const rule = getFileRule({ type: "font/woff2" }, PRODUCTION_RULES, false);
  assert.ok(rule);
  assert.strictEqual(rule.id, "catchall");
});
