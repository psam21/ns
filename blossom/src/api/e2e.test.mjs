import { createHash } from "node:crypto";
import { createServer } from "node:http";
import { mkdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";
import assert from "node:assert/strict";
import { finalizeEvent, generateSecretKey } from "nostr-tools";

const dataDir = "/tmp/blossom-e2e/data";
process.env.BLOSSOM_CONFIG = "e2e-config.yml";
process.env.BLOSSOM_TEST_MODE = "1";
await rm(dataDir, { recursive: true, force: true });

const { app } = await import("../../build/index.js");
const { config } = await import("../../build/config.js");
const { default: storage } = await import("../../build/storage/index.js");
const { db } = await import("../../build/db/db.js");

const secretKey = generateSecretKey();
const authHeader = (type, hash, method = "PUT", url = "") => {
  const event = finalizeEvent(
    {
      kind: 24242,
      created_at: Math.floor(Date.now() / 1000),
      tags: [
        ["t", type],
        ["expiration", String(Math.floor(Date.now() / 1000) + 300)],
        ["x", hash],
      ],
      content: "",
    },
    secretKey,
  );
  return `Nostr ${Buffer.from(JSON.stringify(event)).toString("base64")}`;
};

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

async function startServer() {
  const server = createServer(app.callback());
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Test server did not expose a port");
  return { server, baseURL: `http://127.0.0.1:${address.port}` };
}

test("Blossom HTTP workflow supports current and legacy local objects", async () => {
  const { server, baseURL } = await startServer();
  config.storage.removeWhenNoOwners = true;
  let failed = false;

  try {
    const uploadBody = Buffer.from("blossom e2e upload");
    const uploadHash = sha256(uploadBody);
    const uploadResponse = await fetch(`${baseURL}/upload`, {
      method: "PUT",
      headers: {
        Authorization: authHeader("upload", uploadHash),
        "Content-Type": "text/plain",
        "X-Sha-256": uploadHash,
        Connection: "close",
      },
      body: uploadBody,
    });
    assert.equal(uploadResponse.status, 200);
    assert.equal((await uploadResponse.json()).sha256, uploadHash);

    const downloadResponse = await fetch(`${baseURL}/${uploadHash}.txt`);
    assert.equal(downloadResponse.status, 200);
    assert.deepEqual(Buffer.from(await downloadResponse.arrayBuffer()), uploadBody);

    const deleteResponse = await fetch(`${baseURL}/${uploadHash}.txt`, {
      method: "DELETE",
      headers: { Authorization: authHeader("delete", uploadHash) },
    });
    assert.equal(deleteResponse.status, 200);
    await deleteResponse.arrayBuffer();

    const deletedResponse = await fetch(`${baseURL}/${uploadHash}.txt`);
    assert.equal(deletedResponse.status, 404);
    await deletedResponse.arrayBuffer();

    const legacyBody = Buffer.from("legacy object without metadata");
    const legacyHash = sha256(legacyBody);
    await mkdir(path.join(dataDir, "blobs"), { recursive: true });
    await writeFile(path.join(dataDir, "blobs", `${legacyHash}.txt`), legacyBody);
    await storage.setup();

    const legacyResponse = await fetch(`${baseURL}/${legacyHash}.txt`);
    assert.equal(legacyResponse.status, 200);
    assert.deepEqual(Buffer.from(await legacyResponse.arrayBuffer()), legacyBody);

    const missingResponse = await fetch(`${baseURL}/${"f".repeat(64)}.txt`);
    assert.equal(missingResponse.status, 404);
    await missingResponse.arrayBuffer();
  } catch (error) {
    failed = true;
    throw error;
  } finally {
    server.closeIdleConnections();
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
    db.close();
    process.exit(failed ? 1 : 0);
  }
});
