import { NDKKind } from "@nostr-dev-kit/ndk";
import { npubEncode } from "nostr-tools/nip19";

import { BlobPointer, BlobSearch } from "../types.js";
import logger from "../logger.js";
import ndk from "../ndk.js";

const log = logger.extend("nostr-discovery");
const NOSTR_DISCOVERY_TIMEOUT_MS = 5_000;

async function fetchEventsWithTimeout(hash: string) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const eventsPromise = ndk.fetchEvents({
      kinds: [NDKKind.Media],
      "#x": [hash],
    });
    const timeoutPromise = new Promise<never>((_, reject) => {
      timer = setTimeout(() => reject(new Error("Nostr discovery timed out")), NOSTR_DISCOVERY_TIMEOUT_MS);
    });
    return await Promise.race([eventsPromise, timeoutPromise]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

export async function search(search: BlobSearch) {
  log("Looking for", search.hash);
  const pointers: BlobPointer[] = [];

  let events: Awaited<ReturnType<typeof fetchEventsWithTimeout>>;
  try {
    events = await fetchEventsWithTimeout(search.hash);
  } catch (error) {
    log("Nostr discovery failed", error instanceof Error ? error.message : String(error));
    return pointers;
  }

  // try to use the 1063 events
  if (events.size > 0) {
    for (const event of events) {
      log(`Found 1063 event by ${npubEncode(event.pubkey)}`);
      const url = event.tags.find((t) => t[0] === "url")?.[1];
      const type = event.tags.find((t) => t[0] === "m")?.[1];
      const infohash = event.tags.find((t) => t[0] === "i")?.[1];
      const sizeStr = event.tags.find((t) => t[0] === "size")?.[1];
      const size = sizeStr ? parseInt(sizeStr) : undefined;
      const magnet = event.tags.find((t) => t[0] === "magnet")?.[1];

      if (!size) throw new Error("Missing size");

      if (url) {
        try {
          pointers.push({
            kind: "http",
            hash: search.hash,
            url: new URL(url).toString(),
            type: type,
            metadata: { pubkey: event.pubkey },
            size,
          });
        } catch (e) {}
      }

      if (magnet || infohash) {
        pointers.push({
          kind: "torrent",
          hash: search.hash,
          magnet,
          infohash,
          type: type,
          metadata: { pubkey: event.pubkey },
          size,
        });
      }
    }
  }

  return pointers;
}

export async function getUserCDNList(pubkey: string) {
  const events = await ndk.fetchEvents({
    kinds: [10063 as number],
    authors: [pubkey],
  });

  const cdns = new Set<string>();
  for (const event of events) {
    for (const t of event.tags) {
      if (t[0] === "r" && t[1]) {
        try {
          const url = new URL(t[1]);
          cdns.add(url.toString());
        } catch (e) {}
      }
    }
  }
  return Array.from(cdns);
}
