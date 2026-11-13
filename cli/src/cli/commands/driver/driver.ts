import * as driving from "../../../driver";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { passcodeOptions, udidDeviceOption } from "../flags";

export const driver = defineCommand({
  name: "driver",
  short: tr("Serve the Swipt API and MCP for a device from this computer"),
  description: [
    tr(
      "Only one can control the device at a time: Swipt on it, or the driver on one computer.",
    ),
  ],
  args: [0, 0],
  options: {
    ...udidDeviceOption,
    ...passcodeOptions(
      tr("passcode to unlock the device automatically; empty forgets it"),
    ),
  },
  run: driving.run,
});
