import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
const result = spawnSync(process.execPath, [fileURLToPath(import.meta.resolve("next/dist/bin/next")), "build"], {
  stdio: "inherit", env: { ...process.env, NEXT_EXPORT: "1" },
});
if (result.error) throw result.error;
process.exit(result.status ?? 1);
