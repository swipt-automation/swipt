import type { Client } from "../client";
import type { Context } from "./command";

export const session = async <Result>(
  context: Pick<Context<{ device?: string }>, "options" | "signal" | "target">,
  run: (client: Client) => Result,
) => run(await (await import("../connection")).attach(context));
