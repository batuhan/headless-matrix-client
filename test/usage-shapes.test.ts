import { BeeperDesktop } from "@beeper/desktop-api";

import { createEmbeddedFetch, withEmbedded } from "../src/client.js";

async function usage() {
  const embedded = await createEmbeddedFetch({
    runtime: false,
  });

  const sdk = new BeeperDesktop({
    baseURL: embedded.baseURL,
    accessToken: "token",
    fetch: embedded.fetch,
  });
  void sdk;

  const wrappedCtor = await withEmbedded(BeeperDesktop, {
    runtime: false,
    sdkOptions: { accessToken: "token" },
  });
  void wrappedCtor.sdk;

  const existing = new BeeperDesktop({ accessToken: "token" });
  const wrappedInstance = await withEmbedded(existing, {
    runtime: false,
  });
  void wrappedInstance.sdk;
}

void usage;
