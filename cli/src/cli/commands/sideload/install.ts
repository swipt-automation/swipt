import { tr } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";
import { nightlyOption, passcodeOptions, setupOptions } from "../flags";

export const install = defineCommand({
  name: "install",
  short: tr("Install Swipt"),
  args: [0, 0],
  options: {
    ...passcodeOptions(tr("set up auto-unlock with this passcode")),
    ...nightlyOption,
    force: {
      type: "boolean",
      help: tr("install over the same or a later build"),
    },
    ...setupOptions,
  },
  run: sideload.install,
});
