import { t } from "../../../i18n";
import { defineCommand } from "../../command";
import { setupStepOptions, sideloadPasscodeOptions } from "../flags";

export const setup = defineCommand({
  name: "setup",
  short: t("commands.sideload.setup.description"),
  args: [0, 0],
  options: {
    ...sideloadPasscodeOptions(t("commands.sideload.passcode")),
    ...setupStepOptions,
  },
  run: async (context) => (await import("../../../sideload")).setup(context),
});
