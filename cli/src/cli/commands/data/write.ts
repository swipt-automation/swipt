import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { operandCount } from "./operand";

export const write = (operand, files) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "write",
    use: `write ${operand}PATH [FILE | -]`,
    short: tr("Write a data file"),
    args: [before + 1, before + 2],
    run: files.write,
  });
};
