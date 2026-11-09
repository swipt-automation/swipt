import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const call = defineCommand({
  name: "call",
  use: "call ID ACTION",
  short: tr("Call a swiplet action"),
  args: [2, 2],
  options: {
    input: {
      type: "string",
      value: "JSON",
      help: tr("input JSON, @FILE or -"),
    },
  },
  run: device.call,
});
