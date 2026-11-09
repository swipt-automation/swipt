import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { session } from "../../session";
import { operandCount } from "./operand";

const clearFiles = (target) => (context) =>
  session(context, async (client) => {
    const { id } = target(context.args);
    const { files } = await client.swiplets.listDataFiles(
      { swipletId: id },
      { signal: context.signal },
    );
    for (const file of files) {
      await client.swiplets.removeDataFile(
        { swipletId: id, path: file.path, recursive: true },
        { signal: context.signal },
      );
    }
  });

export const clear = (operand, target) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "clear",
    use: `clear ${operand}`.trimEnd(),
    short: tr("Remove every data file, keeping the data directory"),
    args: [before, before],
    run: clearFiles(target),
  });
};
