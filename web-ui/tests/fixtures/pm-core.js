// Synthetic schema-handshake server only. Business responses are explicit browser fixtures.
import http from "node:http";
import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../../src/lib/config.js";
const digest = await getExpectedCommandRegistryDigest();
const server = http.createServer((req, res) => {
  const path = new URL(req.url, "http://localhost").pathname;
  const data =
    path === "/meta/handshake" || path === "/version"
      ? {
          schema_version: EXPECTED_SCHEMA_VERSION,
          command_registry_digest: digest,
          api_version: "v1",
          core_version: "synthetic-ui-test",
          dev_actor_mode: true,
        }
      : { error: "No business fixture configured" };
  res.writeHead(path === "/meta/handshake" || path === "/version" ? 200 : 404, {
    "content-type": "application/json",
  });
  res.end(JSON.stringify(data));
});
server.listen(Number(process.env.PM_CORE_TEST_PORT || 4382), "127.0.0.1");
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => server.close());
