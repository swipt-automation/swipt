import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { deviceOption } from "../flags";

export const develop = defineCommand({
  name: "develop",
  use: "develop [PROJECT]",
  short: tr("Build, install and follow a swiplet project as it changes"),
  args: [0, 1],
  options: {
    ...deviceOption,
    "no-logs": { type: "boolean", help: tr("without logs") },
  },
  run: device.develop,
});
