import { t } from "../../../i18n";
import { defineCommand } from "../../command";

export const remove = defineCommand({
  name: "remove",
  use: "remove NAME",
  short: t("commands.device.remove.description"),
  args: [1, 1],
  run: async (context) => (await import("../../../connection")).remove(context),
});
