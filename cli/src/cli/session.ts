import type { Client } from "../client";
import { attach } from "../connection";

export const session = <Result>(
  context: Parameters<typeof attach>[0],
  run: (client: Client) => Result,
) => attach(context).then(run);
