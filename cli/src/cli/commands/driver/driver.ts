import * as serving from "../../../serve";
import { t } from "../../../i18n";
import { defineCommand } from "../../command";
import { passcodeOptions, udidDeviceOption } from "../flags";

export const driver = defineCommand({
  name: "driver",
  short: t("commands.driver.description"),
  description: [t("commands.driver.details")],
  args: [0, 0],
  options: {
    ...udidDeviceOption,
    ...passcodeOptions(t("commands.driver.passcode")),
    token: {
      type: "string",
      value: "TOKEN",
      help: t("commands.driver.token"),
    },
    "random-token": {
      type: "boolean",
      help: t("commands.driver.randomToken"),
    },
  },
  run: serving.run,
});
