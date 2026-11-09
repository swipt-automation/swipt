import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const show = defineCommand({
  name: "show",
  use: "show ID",
  short: tr("Show a swiplet"),
  args: [1, 1],
  run: device.show,
});
