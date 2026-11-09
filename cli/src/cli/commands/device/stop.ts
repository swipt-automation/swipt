import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { deviceOption } from "../flags";

export const stop = defineCommand({
  name: "stop",
  short: tr("Stop the Swipt server"),
  args: [0, 0],
  options: deviceOption,
  run: device.stop,
});
