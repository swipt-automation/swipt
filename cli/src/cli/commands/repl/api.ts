import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { session } from "../../session";

const printDeclarations = (context) =>
  session(context, async (client) => {
    process.stdout.write(
      (await client.repl.getDeclarations({}, { signal: context.signal }))
        .typescript,
    );
  });

export const api = defineCommand({
  name: "api",
  short: tr("Print the TypeScript declarations of the globals and modules"),
  args: [0, 0],
  run: printDeclarations,
});
