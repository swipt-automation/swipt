import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { session } from "../../session";
import { operandCount } from "./operand";

const removeFile = (target) => (context) =>
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

export const remove = (operand, target) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "remove",
    use: `remove ${operand}PATH`,
    short: tr("Remove a data file"),
    args: [before + 1, before + 1],
    options: {
      recursive: {
        type: "boolean",
        help: tr("include directory contents"),
      },
    },
    run: removeFile(target),
  });
};
