import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function render() {
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}`);
  const { default: worker } = await import(workerUrl.href);
  return worker.fetch(
    new Request("http://localhost/", { headers: { accept: "text/html" } }),
    { ASSETS: { fetch: async () => new Response("Not found", { status: 404 }) } },
    { waitUntil() {}, passThroughOnException() {} },
  );
}

test("server-renders the Wasted Cycles product page", async () => {
  const response = await render();
  assert.equal(response.status, 200);
  const html = await response.text();
  assert.match(html, /<title>Wasted Cycles/);
  assert.match(html, /Your agents are fast/);
  assert.match(html, /Your harness is not/);
  assert.match(html, /raw\.githubusercontent\.com\/zozo123\/wasted-cycles\/main\/run/);
  assert.match(html, /CODEX/);
  assert.match(html, /CLAUDE CODE/);
  assert.match(html, /CURSOR/);
  assert.match(html, /GROK BUILD/);
  assert.doesNotMatch(html, /codex-preview|react-loading-skeleton|Starter Project/);
});

test("GitHub Pages artifact contains the core promise and social image", async () => {
  const [html, script] = await Promise.all([
    readFile(new URL("../docs/index.html", import.meta.url), "utf8"),
    readFile(new URL("../docs/app.js", import.meta.url), "utf8"),
  ]);
  assert.match(html, /Find where coding-agent runs stop coding/);
  assert.match(html, /og\.png/);
  assert.match(html, /copy-command/);
  assert.match(script, /navigator\.clipboard\.writeText/);
});
