import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("../src/views/ProxyPool.vue", import.meta.url), "utf8");

test("uses the proxy operations workspace layout", () => {
  assert.match(source, /class="proxy-overview"/);
  assert.match(source, /class="proxy-stat-card"/);
  assert.match(source, /class="proxy-workspace"/);
  assert.match(source, /class="proxy-list-panel"/);
  assert.match(source, /class="quick-import-panel"/);
  assert.match(source, /class="health-queue-panel"/);
});

test("keeps the import action wired inside the quick import panel", () => {
  const importPanelStart = source.indexOf('class="quick-import-panel"');
  assert.ok(importPanelStart >= 0, "quick import panel must exist");

  const importPanelTemplate = source.slice(importPanelStart);
  assert.match(importPanelTemplate, /v-model:value="proxiesText"/);
  assert.match(importPanelTemplate, /@click="importProxies"/);
  assert.match(importPanelTemplate, /:disabled="!proxiesText\.trim\(\)"/);
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

test("bounds the node list and wires client pagination", () => {
  assert.match(source, /class="proxy-table-viewport"/);
  assert.match(source, /<n-pagination/);
  assert.match(source, /const pageSize = ref\(8\)/);
  assert.match(source, /const currentPage = ref\(1\)/);
  assert.match(source, /const start = \(currentPage\.value - 1\) \* pageSize\.value/);
  assert.match(source, /slice\(start, start \+ pageSize\.value\)/);
  assert.match(source, /overflow:\s*auto/);
  assert.match(source, /height:\s*clamp\(/);
});

test("makes the quick import action navigate to a usable input", () => {
  assert.match(source, /function openQuickImport\(\)/);
  assert.match(source, /@click="openQuickImport"/);
  assert.match(source, /scrollIntoView\(\{ behavior: "smooth", block: "center" \}\)/);
  assert.match(source, /textarea\?\.focus\(\)/);
  assert.match(source, /quickImportAction/);
});

test("aligns the workspace columns and gives the proxy page a logo", () => {
  assert.match(source, /class="page-title-group"/);
  assert.match(source, /class="proxy-page-logo"/);
  assert.match(source, /align-self:\s*stretch/);
  assert.match(source, /grid-template-columns:\s*minmax\(0, 1fr\) 326px/);
});

test("keeps the right-side operations as one continuous aligned stack", () => {
  const sideStyle = source.match(/\.proxy-side-column\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
  assert.match(source, /class="proxy-side-column"/);
  assert.match(sideStyle, /display:\s*flex/);
  assert.match(sideStyle, /flex-direction:\s*column/);
  assert.match(sideStyle, /gap:\s*0/);
  assert.match(
    source,
    /border-radius:\s*var\(--border-radius-lg\)\s+var\(--border-radius-lg\)\s+0\s+0/
  );
  assert.match(
    source,
    /border-radius:\s*0\s+0\s+var\(--border-radius-lg\)\s+var\(--border-radius-lg\)/
  );
});
