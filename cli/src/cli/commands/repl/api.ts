import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const api = defineCommand({
  name: "api",
  short: tr("Print the TypeScript declarations of the globals and modules"),
  args: [0, 0],
  run: device.api,
});
