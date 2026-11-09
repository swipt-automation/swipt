import { tr } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";

export const uninstall = defineCommand({
  name: "uninstall",
  short: tr("Remove Swipt"),
  args: [0, 0],
  run: sideload.uninstall,
});
