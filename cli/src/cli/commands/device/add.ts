import * as connection from "../../../connection";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

export const add = defineCommand({
  name: "add",
  use: "add URL",
  short: tr(
    "Add a device by its URL; needed when it is remote or requires a token",
  ),
  args: [1, 1],
  run: connection.add,
});
