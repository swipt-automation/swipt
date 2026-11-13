import { tr } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { deviceOption } from "../flags";

const stopServing = (context: Context<{ device?: string }>) =>
  session(context, async (client) => {
    await client.system.shutdown({}, { signal: context.signal });
    console.log(tr("stopped"));
  });

export const stop = defineCommand({
  name: "stop",
  short: tr("Stop serving the Swipt API"),
  args: [0, 0],
  options: deviceOption,
  run: stopServing,
});
