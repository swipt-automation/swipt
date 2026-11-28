import { t } from "../../../i18n";
import { defineCommand } from "../../command";
import { nightlyOption, setupOptions, sideloadPasscodeOptions } from "../flags";

export const update = defineCommand({
  name: "update",
  short: t("commands.sideload.update.description"),
  args: [0, 0],
  options: {
    ...sideloadPasscodeOptions(t("commands.sideload.update.passcode")),
    ...nightlyOption,
    ...setupOptions,
  },
  run: async (context) => (await import("../../../sideload")).update(context),
});
