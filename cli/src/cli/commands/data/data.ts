import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { clear } from "./clear";
import { list } from "./list";
import type { ResolveTarget } from "./operand";
import { read } from "./read";
import { remove } from "./remove";
import { write } from "./write";

export const data = (operand: string, target: ResolveTarget) =>
  defineCommand({
    name: "data",
    short:
      operand === ""
        ? tr("Manage the files in the data directory of the REPL")
        : tr("Manage the files in the data directory of a swiplet"),
    commands: [
      list(operand, target),
      read(operand, target),
      write(operand, target),
      remove(operand, target),
      clear(operand, target),
    ],
  });
