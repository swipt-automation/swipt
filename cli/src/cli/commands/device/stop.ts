import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { session } from "../../session";
import { deviceOption } from "../flags";

const stopServer = (context) =>
  session(context, async (client) => {
    await client.system.shutdown({}, { signal: context.signal });
    console.log(tr("stopped"));
  });

export const stop = defineCommand({
  name: "stop",
  short: tr("Stop the Swipt server"),
  args: [0, 0],
  options: deviceOption,
  run: stopServer,
});
