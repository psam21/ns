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
