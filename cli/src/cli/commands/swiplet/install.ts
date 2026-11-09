import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const install = defineCommand({
  name: "install",
  use: "install ARCHIVE",
  short: tr("Install a swiplet archive"),
  args: [1, 1],
  run: device.install,
});
