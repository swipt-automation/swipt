import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { operandCount } from "./operand";

export const list = (operand, files) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "list",
    use: `list ${operand}[PATH]`,
    short: tr("List data files"),
    args: [before, before + 1],
    options: {
      recursive: { type: "boolean", help: tr("include subdirectories") },
    },
    run: files.list,
  });
};
