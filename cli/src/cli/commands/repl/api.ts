import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";

const printDeclarations = (context: Context<{ device?: string }>) =>
  session(context, async (client) => {
    const [{ printData }, { typescript }] = await Promise.all([
      import("../../../output"),
      client.repl.getDeclarations({}, { signal: context.signal }),
    ]);
    printData(Buffer.from(typescript));
  });

export const api = defineCommand({
  name: "api",
  short: t("commands.repl.api.description"),
  args: [0, 0],
  run: printDeclarations,
});
