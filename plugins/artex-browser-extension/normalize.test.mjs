import test from "node:test";
import assert from "node:assert/strict";
import { normalize, backendOrigin } from "./normalize.mjs";

test("HTTP/2 pseudo headers removed, credentials require opt-in", () => {
  const entry = { request: { url: "https://example.test/a", method: "GET", headers: [{ name: ":authority", value: "example.test" }, { name: "Cookie", value: "private" }, { name: "Accept", value: "application/json" }] }, response: { status: 200, headers: [{ name: "Set-Cookie", value: "private" }] } };
  assert.deepEqual(normalize(entry).headers, [{ name: "Accept", value: "application/json" }]);
  assert.equal(normalize(entry).response_headers.length, 0);
  assert.equal(normalize(entry, true).headers.length, 2);
});
test("backend access is an exact secure origin", () => {
  assert.equal(backendOrigin("http://127.0.0.1:8787/"), "http://127.0.0.1:8787");
  assert.equal(backendOrigin("https://artex.example.test"), "https://artex.example.test");
  for (const url of ["http://remote.example.test", "https://name:pass@example.test", "file:///etc/passwd", "https://example.test/api"]) assert.throws(() => backendOrigin(url));
});
