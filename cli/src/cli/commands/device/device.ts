import { t } from "../../../i18n";
import { defineCommand } from "../../command";
import { add } from "./add";
import { list } from "./list";
import { logs } from "./logs";
import { remove } from "./remove";
import { stop } from "./stop";

export const device = defineCommand({
  name: "device",
  short: t("commands.device.description"),
  commands: [list, add, remove, stop, logs],
});
