import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";

const resetSession = (context: Context<{ device?: string }>) =>
  session(context, (client) =>
    client.repl.reset({}, { signal: context.signal }),
  );

export const reset = defineCommand({
  name: "reset",
  short: t("commands.repl.reset.description"),
  args: [0, 0],
  run: resetSession,
});
