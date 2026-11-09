import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const version = defineCommand({
  name: "version",
  short: tr("Print the version"),
  args: [0, 0],
  run: device.printVersion,
});
