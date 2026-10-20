import fs from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
const result = spawnSync(process.execPath, [fileURLToPath(import.meta.resolve("@biomejs/biome/bin/biome")), "lint", "--reporter=json"], { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 });
if (result.error) throw result.error;
let report;
try { report = JSON.parse(result.stdout); } catch { process.stderr.write(result.stderr || result.stdout); process.exit(1); }
const key = (d) => [d.location.path.replaceAll("\\", "/"), d.category, d.message].join(" | ");
if (report.summary.diagnosticsNotPrinted > 0) {
  console.error("Incomplete lint diagnostics; refusing to compare a partial report.");
  process.exit(1);
}
const errors = report.diagnostics.filter((d) => ["error", "fatal"].includes(d.severity));
const current = {};
for (const d of errors) current[key(d)] = (current[key(d)] ?? 0) + 1;
const baselinePath = fileURLToPath(new URL("../lint-baseline.json", import.meta.url));
if (process.argv.includes("--update-baseline")) {
  fs.writeFileSync(baselinePath, JSON.stringify(Object.fromEntries(Object.entries(current).sort()), null, 2) + "\n");
  console.log(`Recorded ${errors.length} existing errors; review this baseline diff.`);
  process.exit(0);
}
const baseline = JSON.parse(fs.readFileSync(baselinePath, "utf8"));
const regressions = Object.entries(current).filter(([k, count]) => count > (baseline[k] ?? 0));
const stale = Object.keys(baseline).filter((k) => (current[k] ?? 0) < baseline[k]);
if (regressions.length || stale.length) {
  for (const [k, count] of regressions) console.error(`New lint errors (${count}): ${k}`);
  for (const k of stale) console.error(`Resolved lint debt: remove or reduce baseline entry: ${k}`);
  process.exit(1);
}
if (result.status !== 0 && !errors.length) { process.stderr.write(result.stderr); process.exit(1); }
console.log(`Lint gate passed: ${errors.length} documented existing errors, ${report.summary.warnings} warnings. No new errors.`);
