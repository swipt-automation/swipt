import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { session } from "../../session";

const resetSession = (context) =>
  session(context, (client) =>
    client.repl.reset({}, { signal: context.signal }),
  );

export const reset = defineCommand({
  name: "reset",
  short: tr("Reset the session"),
  args: [0, 0],
  run: resetSession,
});
