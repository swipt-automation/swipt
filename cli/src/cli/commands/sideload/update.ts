import { t } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";
import { nightlyOption, passcodeOptions, setupOptions } from "../flags";

export const update = defineCommand({
  name: "update",
  short: t("commands.sideload.update.description"),
  args: [0, 0],
  options: {
    ...passcodeOptions(t("commands.sideload.update.passcode")),
    ...nightlyOption,
    ...setupOptions,
  },
  run: sideload.update,
});
