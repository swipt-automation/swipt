import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { add } from "./add";
import { list } from "./list";
import { logs } from "./logs";
import { remove } from "./remove";
import { stop } from "./stop";

export const device = defineCommand({
  name: "device",
  short: tr("Manage the devices and read their logs"),
  commands: [list, add, remove, stop, logs],
});
