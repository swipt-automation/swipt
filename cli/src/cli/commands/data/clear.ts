import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { operandCount } from "./operand";

export const clear = (operand, files) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "clear",
    use: `clear ${operand}`.trimEnd(),
    short: tr("Remove every data file, keeping the data directory"),
    args: [before, before],
    run: files.clear,
  });
};
