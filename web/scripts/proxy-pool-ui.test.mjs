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

test("keeps proxy import placeholders compatible with Vue i18n", async () => {
  for (const locale of ["en-US", "ja-JP", "zh-CN"]) {
    const localeSource = await readFile(
      new URL(`../src/locales/${locale}.ts`, import.meta.url),
      "utf8"
    );
    const proxyPoolStart = localeSource.lastIndexOf("  proxyPool: {");
    const proxyPoolEnd = localeSource.indexOf("\n  },", proxyPoolStart);
    const proxyPoolMessages = localeSource.slice(proxyPoolStart, proxyPoolEnd);
    const placeholder = proxyPoolMessages.match(/importPlaceholder:\s*"([^"]*)"/);

    assert.ok(placeholder, `${locale} proxy import placeholder must exist`);
    assert.doesNotMatch(
      placeholder[1],
      /@/,
      `${locale} placeholder must not use Vue i18n link syntax`
    );
  }
});
