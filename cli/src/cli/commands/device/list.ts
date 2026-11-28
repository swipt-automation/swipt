import { t } from "../../../i18n";
import { defineCommand } from "../../command";

export const list = defineCommand({
  name: "list",
  short: t("commands.device.list.description"),
  args: [0, 0],
  run: async (context) => (await import("../../../connection")).list(context),
});
