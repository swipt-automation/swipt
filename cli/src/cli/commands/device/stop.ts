import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { deviceOption } from "../flags";

const stopServing = (context: Context<{ device?: string }>) =>
  session(context, async (client) => {
    await client.system.shutdown({}, { signal: context.signal });
    console.log(t("commands.device.stop.stopped"));
  });

export const stop = defineCommand({
  name: "stop",
  short: t("commands.device.stop.description"),
  args: [0, 0],
  options: deviceOption,
  run: stopServing,
});
