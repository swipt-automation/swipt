import * as connection from "../../../connection";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const list = defineCommand({
  name: "list",
  short: tr("List the devices, adding the ones found on the local network"),
  args: [0, 0],
  run: connection.list,
});
