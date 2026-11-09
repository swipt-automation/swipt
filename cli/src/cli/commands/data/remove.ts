import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { operandCount } from "./operand";

export const remove = (operand, files) => {
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
    run: files.remove,
  });
};
