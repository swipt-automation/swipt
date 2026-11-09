import * as device from "../../../../device";
import { tr } from "../../../../i18n";
import { defineCommand } from "../../../command";

export const watch = defineCommand({
  name: "watch",
  use: "watch [ID]",
  short: tr("Watch swiplet services"),
  args: [0, 1],
  run: device.serviceWatch,
});
