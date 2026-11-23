import { t } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";
import { nightlyOption, setupOptions, sideloadPasscodeOptions } from "../flags";

export const install = defineCommand({
  name: "install",
  short: t("commands.sideload.install.description"),
  args: [0, 0],
  options: {
    ...sideloadPasscodeOptions(t("commands.sideload.passcode")),
    ...nightlyOption,
    force: {
      type: "boolean",
      help: t("commands.sideload.install.force"),
    },
    ...setupOptions,
  },
  run: sideload.install,
});
