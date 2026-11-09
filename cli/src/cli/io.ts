import fs from "node:fs";

export const uploadChunk = 256 << 10;

export const readStdin = () => fs.readFileSync(0);
