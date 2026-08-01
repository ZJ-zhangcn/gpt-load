import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const readProjectFile = relativePath => readFile(new URL(relativePath, import.meta.url), "utf8");

test("uses the shipped upstream version as the footer update baseline", async () => {
  const source = await readProjectFile("../src/services/version.ts");

  assert.match(
    source,
    /import\.meta\.env\.VITE_UPSTREAM_VERSION\s*\|\|\s*import\.meta\.env\.VITE_VERSION/,
    "the upstream baseline must take precedence over the fork build revision"
  );
});

test("injects the reachable upstream tag into every production image build", async () => {
  const [dockerfile, workflow, packageJson] = await Promise.all([
    readProjectFile("../../Dockerfile"),
    readProjectFile("../../.github/workflows/docker-build.yml"),
    readProjectFile("../package.json"),
  ]);

  assert.match(dockerfile, /ARG UPSTREAM_VERSION=/);
  assert.match(dockerfile, /VITE_UPSTREAM_VERSION=\$\{UPSTREAM_VERSION\}/);
  assert.match(workflow, /fetch-depth:\s*0/);
  assert.match(workflow, /id:\s*upstream-version/);
  assert.match(workflow, /\+refs\/tags\/v\*:refs\/tags\/v\*/);
  assert.match(workflow, /git tag --merged "\$GITHUB_SHA" --list 'v\[0-9\]\*'/);
  assert.match(workflow, /UPSTREAM_VERSION=\$\{\{ steps\.upstream-version\.outputs\.version \}\}/);
  assert.equal(JSON.parse(packageJson).scripts["test:unit"], "node --test scripts/*.test.mjs");
});
