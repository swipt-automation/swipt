import { t } from "../../../../i18n";
import { defineCommand } from "../../../command";
import { setService } from "./services";

export const enable = defineCommand({
  name: "enable",
  use: "enable ID",
  short: t("commands.swiplet.service.enable.description"),
  args: [1, 1],
  options: {
    watch: { type: "boolean", help: t("commands.swiplet.service.watchState") },
  },
  run: setService(true),
});
