import { t } from "../../../i18n";
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
        ? t("commands.data.replDescription")
        : t("commands.data.swipletDescription"),
    commands: [
      list(operand, target),
      read(operand, target),
      write(operand, target),
      remove(operand, target),
      clear(operand, target),
    ],
  });
