import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("../src/views/ProxyPool.vue", import.meta.url), "utf8");

test("keeps the proxy import form inside the visible nodes card", () => {
  const nodesCardStart = source.indexOf('<n-card class="nodes-card"');
  assert.ok(nodesCardStart >= 0, "nodes card must exist");

  const nodesCardTemplate = source.slice(nodesCardStart);
  assert.match(nodesCardTemplate, /<section class="proxy-import"/);
  assert.match(nodesCardTemplate, /v-model:value="proxiesText"/);
  assert.match(nodesCardTemplate, /@click="importProxies"/);
  assert.doesNotMatch(source, /<n-card class="import-card"/);
});
