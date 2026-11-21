import fs from "node:fs";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "../..");

export const checkout = fs.existsSync(path.join(root, "go.mod"))
  ? root
  : undefined;
