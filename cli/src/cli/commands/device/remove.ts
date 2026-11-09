import * as connection from "../../../connection";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const remove = defineCommand({
  name: "remove",
  use: "remove NAME",
  short: tr("Forget a device"),
  args: [1, 1],
  run: connection.remove,
});
