import { t } from "../../../i18n";
import * as sideload from "../../../sideload";
import { defineCommand } from "../../command";

export const uninstall = defineCommand({
  name: "uninstall",
  short: t("commands.sideload.uninstall.description"),
  args: [0, 0],
  run: sideload.uninstall,
});
