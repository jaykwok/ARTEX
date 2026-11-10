import assert from "node:assert/strict";
import test from "node:test";
import { activeNavigationUrl } from "../src/lib/navigation.ts";

const urls = ["/function/tasks", "/system/intercept", "/system/intercept/approvals", "/系统/日志"];

test("navigation follows detail routes and exported trailing slashes", () => {
  assert.equal(activeNavigationUrl("/function/tasks/", urls), "/function/tasks");
  assert.equal(activeNavigationUrl("/function/tasks/detail/?id=42&tab=overview", urls), "/function/tasks");
  assert.equal(activeNavigationUrl("/system/intercept/approvals/", urls), "/system/intercept/approvals");
});

test("navigation respects path boundaries and encoded paths", () => {
  assert.equal(activeNavigationUrl("/function/tasks-archive", urls), undefined);
  assert.equal(activeNavigationUrl("/%E7%B3%BB%E7%BB%9F/%E6%97%A5%E5%BF%97/", urls), "/系统/日志");
  assert.equal(activeNavigationUrl("/%invalid/", urls), undefined);
});
