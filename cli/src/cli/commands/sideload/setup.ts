import { t } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";
import { passcodeOptions, setupStepOptions } from "../flags";

export const setup = defineCommand({
  name: "setup",
  short: t("commands.sideload.setup.description"),
  args: [0, 0],
  options: {
    ...passcodeOptions(t("commands.sideload.passcode")),
    ...setupStepOptions,
  },
  run: sideload.setup,
});
