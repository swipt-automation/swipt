import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { type ResolveTarget, operandCount } from "./operand";

const removeFile =
  (target: ResolveTarget) =>
  (context: Context<{ device?: string; recursive?: boolean }>) =>
    session(context, async (client) => {
      const {
        id,
        args: [name],
      } = target(context.args);
      await client.swiplets.removeDataFile(
        { swipletId: id, path: name, recursive: context.options.recursive },
        { signal: context.signal },
      );
    });

export const remove = (operand: string, target: ResolveTarget) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "remove",
    use: `remove ${operand}PATH`,
    short: t("commands.data.remove.description"),
    args: [before + 1, before + 1],
    options: {
      recursive: {
        type: "boolean",
        help: t("commands.data.remove.recursive"),
      },
    },
    run: removeFile(target),
  });
};
