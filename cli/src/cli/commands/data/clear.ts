import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { type ResolveTarget, operandCount } from "./operand";

const clearFiles =
  (target: ResolveTarget) => (context: Context<{ device?: string }>) =>
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

export const clear = (operand: string, target: ResolveTarget) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "clear",
    use: `clear ${operand}`.trimEnd(),
    short: t("commands.data.clear.description"),
    args: [before, before],
    run: clearFiles(target),
  });
};
