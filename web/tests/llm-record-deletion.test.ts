import assert from "node:assert/strict";
import test from "node:test";
import { deleteTaskRecords } from "../src/lib/llm-record-deletion.ts";

for (const [name, message] of [
  ["409 restore protection", "还原未完成，不能删除"],
  ["500 database error", "数据库写入失败"],
  ["network failure", "Failed to fetch"],
]) {
  test(name, async () => {
    const state = { task: "42", selection: "record-1", rows: ["record-1"], dialog: true };
    const before = structuredClone(state);
    const errors: string[] = [], busy: boolean[] = [];
    await deleteTaskRecords(
      async () => { throw new Error(message); },
      () => { state.task = ""; state.selection = ""; state.rows = []; state.dialog = false; },
      (text) => errors.push(text), (value) => busy.push(value),
    );
    assert.deepEqual(state, before);
    assert.deepEqual(errors, [message]);
    assert.deepEqual(busy, [true, false]);
  });
}

test("successful deletion clears selection and refreshes once", async () => {
  let selected = "record-1", refreshes = 0;
  const errors: string[] = [], busy: boolean[] = [];
  await deleteTaskRecords(async () => undefined, () => { selected = ""; refreshes++; },
    (message) => errors.push(message), (value) => busy.push(value));
  assert.equal(selected, "");
  assert.equal(refreshes, 1);
  assert.deepEqual(errors, []);
  assert.deepEqual(busy, [true, false]);
});
