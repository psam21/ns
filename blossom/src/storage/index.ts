import { mkdirp } from "mkdirp";
import { config } from "../config.js";
import { BlobMetadata } from "blossom-server-sdk";
import { LocalStorage, S3Storage, IBlobStorage } from "blossom-server-sdk/storage";
import dayjs from "dayjs";

import { BlobSearch, StoragePointer } from "../types.js";
import db, { blobDB } from "../db/db.js";
import logger from "../logger.js";
import { getExpirationTime } from "../rules/index.js";
import { forgetBlobAccessed, updateBlobAccess } from "../db/methods.js";
import { readUpload, removeUpload, UploadDetails } from "./upload.js";
import { mapParams } from "../admin-api/helpers.js";

/**
 * Convert a rule's `type` pattern (using `*` as wildcard) to a SQL LIKE
 * pattern. Escapes `%`, `_`, and `\` so a rule like `image_/_jpeg` is
 * treated literally (issue #86).
 */
function sqlLikePattern(rule: string): string {
  return rule
    .replace(/\\/g, "\\\\")
    .replace(/%/g, "\\%")
    .replace(/_/g, "\\_")
    .replace(/\*/g, "%");
}

async function createStorage() {
  if (config.storage.backend === "local") {
    await mkdirp(config.storage.local!.dir);
    return new LocalStorage(config.storage.local!.dir);
  } else if (config.storage.backend === "s3") {
    const s3 = new S3Storage(
      config.storage.s3!.endpoint,
      config.storage.s3!.accessKey,
      config.storage.s3!.secretKey,
      config.storage.s3!.bucket,
      config.storage.s3,
    );
    s3.publicURL = config.storage.s3!.publicURL;
    return s3;
  } else throw new Error("Unknown cache backend " + config.storage.backend);
}

const log = logger.extend("storage");

const storage: IBlobStorage = await createStorage();

log("Setting up storage");
await storage.setup();

export async function searchStorage(search: BlobSearch): Promise<StoragePointer | undefined> {
  const blob = await blobDB.getBlob(search.hash);
  if (blob && (await storage.hasBlob(search.hash))) {
    const type = blob.type || (await storage.getBlobType(search.hash));
    const size = blob.size || (await storage.getBlobSize(search.hash));
    log("Found", search.hash);
    return { kind: "storage", hash: search.hash, type: type, size };
  }
}

export function getStorageRedirect(pointer: StoragePointer) {
  const publicURL = config.storage.s3?.publicURL;
  if (storage instanceof S3Storage && publicURL) {
    const object = storage.objects.find((obj) => obj.name.startsWith(pointer.hash));
    if (object) return publicURL + object.name;
  }
}

export async function readStoragePointer(pointer: StoragePointer) {
  return await storage.readBlob(pointer.hash);
}

export async function addFromUpload(upload: UploadDetails, type?: string) {
  type = type || upload.type;

  let blob: BlobMetadata;

  if (!blobDB.hasBlob(upload.sha256)) {
    log("Saving", upload.sha256, type, upload.size);
    await storage.writeBlob(upload.sha256, readUpload(upload), type);
    await removeUpload(upload);

    const now = dayjs().unix();
    blob = blobDB.addBlob({ sha256: upload.sha256, size: upload.size, type, uploaded: now });
    updateBlobAccess(upload.sha256, dayjs().unix());
  } else {
    blob = blobDB.getBlob(upload.sha256);
    await removeUpload(upload);
  }

  return blob;
}

export async function pruneStorage() {
  const now = dayjs().unix();
  const checked = new Set<string>();

  // Bound the number of concurrent storage.removeBlob calls so a large
  // prune pass does not saturate the S3 delete API rate or fork-bomb
  // the process (issue #94). 10 is a conservative default that keeps
  // per-call latency low while leaving headroom for upload traffic.
  const MAX_CONCURRENT_REMOVES = 10;
  let inFlight = 0;
  const removalQueue: Promise<void>[] = [];
  const waitForSlot = async () => {
    while (inFlight >= MAX_CONCURRENT_REMOVES) {
      if (removalQueue.length === 0) break;
      await Promise.race(removalQueue);
    }
  };
  const enqueueRemove = (sha256: string, type: string | undefined, ruleLabel: string) => {
    inFlight++;
    let p!: Promise<void>;
    const work = (async () => {
      try {
        log("Removing", sha256, type, "because", ruleLabel);
        await blobDB.removeBlob(sha256);
        if (await storage.hasBlob(sha256)) await storage.removeBlob(sha256);
        forgetBlobAccessed(sha256);
      } finally {
        inFlight--;
        // Resolve self so waitForSlot can re-evaluate.
        const idx = removalQueue.indexOf(p);
        if (idx >= 0) removalQueue.splice(idx, 1);
      }
    })();
    p = work;
    removalQueue.push(p);
    return p;
  };
  const drainRemovals = async () => {
    while (inFlight > 0) {
      if (removalQueue.length === 0) break;
      await Promise.race(removalQueue);
    }
  };

  /** Remove all blobs that no longer fall under any rules */
  for (const rule of config.storage.rules) {
    const expiration = getExpirationTime(rule, now);
    let blobs: (BlobMetadata & { pubkey: string; accessed: number | null })[] = [];

    if (rule.pubkeys?.length) {
      blobs = db
        .prepare(
          `
          SELECT blobs.*,owners.pubkey, accessed.timestamp as "accessed"
          FROM blobs
            LEFT JOIN owners ON owners.blob = blobs.sha256
            LEFT JOIN accessed ON accessed.blob = blobs.sha256
          WHERE
            blobs.type LIKE ? AND
            owners.pubkey IN (${Array.from(rule.pubkeys).fill("?").join(", ")})
        `,
        )
        .all(sqlLikePattern(rule.type), ...rule.pubkeys) as (BlobMetadata & {
        pubkey: string;
        accessed: number | null;
      })[];
    } else {
      blobs = db
        .prepare(
          `
          SELECT blobs.*,owners.pubkey, accessed.timestamp as "accessed"
          FROM blobs
            LEFT JOIN owners ON owners.blob = blobs.sha256
            LEFT JOIN accessed ON accessed.blob = blobs.sha256
          WHERE
            blobs.type LIKE ?
        `,
        )
        .all(sqlLikePattern(rule.type)) as (BlobMetadata & {
        pubkey: string;
        accessed: number | null;
      })[];
    }

    let n = 0;
    for (const blob of blobs) {
      if (checked.has(blob.sha256)) continue;

      if ((blob.accessed || blob.uploaded) < expiration) {
        await waitForSlot();
        enqueueRemove(blob.sha256, blob.type, String(config.storage.rules.indexOf(rule)));
      }

      n++;
      checked.add(blob.sha256);
    }
    // Drain any in-flight removes for this rule before moving on so the
    // cron does not leave dangling promises between ticks.
    await drainRemovals();
    if (n > 0) log("Checked", n, "blobs for rule #" + config.storage.rules.indexOf(rule));
  }

  // remove blobs with no owners
  if (config.storage.removeWhenNoOwners) {
    const blobs = db
      .prepare<[], { sha256: string }>(
        `
      SELECT blobs.sha256
      FROM blobs
        LEFT JOIN owners ON owners.blob = sha256
      WHERE owners.blob is NULL
    `,
      )
      .all();

    if (blobs.length > 0) {
      log(`Removing ${blobs.length} because they have no owners`);
      db.prepare<string[]>(`DELETE FROM blobs WHERE sha256 IN ${mapParams(blobs)}`).run(...blobs.map((b) => b.sha256));
    }
  }
}

export default storage;
