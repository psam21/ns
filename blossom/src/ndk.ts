import NDK from "@nostr-dev-kit/ndk";
import { config } from "./config.js";

const ndk = new NDK({
  explicitRelayUrls: config.discovery.nostr.relays,
});

if (process.env.BLOSSOM_TEST_MODE !== "1") {
  ndk.connect();
}

export default ndk;
