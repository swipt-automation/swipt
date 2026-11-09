#!/usr/bin/env node
import { main } from "../cli/cli";

await main();
await Promise.all(
  [process.stdout, process.stderr].map(
    (stream) => new Promise((resolve) => stream.write("", resolve)),
  ),
);
process.exit();
