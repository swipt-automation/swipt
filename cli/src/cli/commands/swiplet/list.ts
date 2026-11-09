import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const list = defineCommand({
  name: "list",
  short: tr("List the swiplets"),
  args: [0, 0],
  run: device.list,
});
