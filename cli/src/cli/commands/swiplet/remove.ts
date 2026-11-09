import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const remove = defineCommand({
  name: "remove",
  use: "remove ID",
  short: tr("Remove a swiplet"),
  args: [1, 1],
  run: device.remove,
});
