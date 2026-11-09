import { tr } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";
import { passcodeOptions, setupStepOptions } from "../flags";

export const setup = defineCommand({
  name: "setup",
  short: tr("Set up Swipt"),
  args: [0, 0],
  options: {
    ...passcodeOptions(tr("set up auto-unlock with this passcode")),
    ...setupStepOptions,
  },
  run: sideload.setup,
});
