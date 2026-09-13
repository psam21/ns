import { test, describe } from "node:test";
import assert from "node:assert/strict";
import HttpErrors from "http-errors";
import { isBlockedAddress, resolvePublicAddresses } from "../../build/helpers/ssrf.js";
import { getBlobURL } from "../../build/helpers/blob.js";
import { isHttpError } from "../../build/helpers/error.js";

await import("../../build/config.js");

 describe("SSRF address filtering", () => {
  test("blocks private, loopback, link-local, and multicast addresses", () => {
    for (const address of ["10.0.0.1", "127.0.0.1", "169.254.1.1", "192.168.1.10", "::1", "fc00::1", "ff02::1"]) {
      assert.equal(isBlockedAddress(address), true, address);
    }
  });

  test("allows a public literal address and returns its family", async () => {
    assert.deepEqual(await resolvePublicAddresses("8.8.8.8"), [{ address: "8.8.8.8", family: 4 }]);
  });

  test("rejects localhost before DNS resolution", async () => {
    await assert.rejects(resolvePublicAddresses("localhost"), /SSRF blocked/);
  });
});

describe("blob URL and error helpers", () => {
  test("builds a URL using the configured public domain", () => {
    const url = getBlobURL({ sha256: "a".repeat(64), type: "image/png" });
    assert.match(url, /a{64}\.png$/);
  });

  test("recognizes HTTP errors and rejects plain errors", () => {
    assert.equal(isHttpError(new HttpErrors.NotFound()), true);
    assert.equal(isHttpError(new Error("plain")), false);
    assert.equal(isHttpError(null), false);
  });
});
