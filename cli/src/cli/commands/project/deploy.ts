import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { deviceOption } from "../flags";

export const deploy = defineCommand({
  name: "deploy",
  use: "deploy [PROJECT]",
  short: tr("Build the project and install it on the device"),
  args: [0, 1],
  options: deviceOption,
  run: device.deployProject,
});
