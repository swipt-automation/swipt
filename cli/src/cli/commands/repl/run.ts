import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const run = defineCommand({
  name: "run",
  use: "run [CODE | FILE [ARG...] | - [ARG...]]",
  short: tr("Run a JavaScript expression or program on the device"),
  description: [
    tr(
      "The first argument is FILE when it names an existing file and CODE otherwise; options go before it.",
    ),
    tr(
      "CODE is an expression whose value is printed. FILE, or standard input given as - or no argument, is a program, and only its console output is printed.",
    ),
  ],
  args: [0, Infinity],
  passthrough: device.isProgram,
  options: {
    output: {
      type: "string",
      value: "DIRECTORY",
      help: tr("image directory (default: a new temporary directory)"),
    },
  },
  run: device.run,
});
