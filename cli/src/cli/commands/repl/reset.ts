import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const reset = defineCommand({
  name: "reset",
  short: tr("Reset the session"),
  args: [0, 0],
  run: device.reset,
});
