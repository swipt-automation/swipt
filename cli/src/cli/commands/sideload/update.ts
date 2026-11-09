import { tr } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";
import { nightlyOption, passcodeOptions, setupOptions } from "../flags";

export const update = defineCommand({
  name: "update",
  short: tr("Update or renew Swipt"),
  args: [0, 0],
  options: {
    ...passcodeOptions(tr("unlock device with this passcode")),
    ...nightlyOption,
    ...setupOptions,
  },
  run: sideload.update,
});
