import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { sideloadOptions } from "../flags";
import { install } from "./install";
import { setup } from "./setup";
import { uninstall } from "./uninstall";
import { update } from "./update";

export const sideload = defineCommand({
  name: "sideload",
  short: tr("Sideload Swipt"),
  options: sideloadOptions,
  commands: [install, update, setup, uninstall],
});
