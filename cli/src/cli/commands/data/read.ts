import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { operandCount } from "./operand";

export const read = (operand, files) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "read",
    use: `read ${operand}PATH`,
    short: tr("Read a data file or directory"),
    args: [before + 1, before + 1],
    options: {
      output: { type: "string", value: "PATH", help: tr("destination") },
    },
    run: files.read,
  });
};
