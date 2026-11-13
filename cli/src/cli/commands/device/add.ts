import * as connection from "../../../connection";
import { t } from "../../../i18n";
import { defineCommand } from "../../command";

export const add = defineCommand({
  name: "add",
  use: "add URL",
  short: t("commands.device.add.description"),
  args: [1, 1],
  run: connection.add,
});
