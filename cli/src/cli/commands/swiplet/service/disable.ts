import { t } from "../../../../i18n";
import { defineCommand } from "../../../command";
import { setService } from "./services";

export const disable = defineCommand({
  name: "disable",
  use: "disable ID",
  short: t("commands.swiplet.service.disable.description"),
  args: [1, 1],
  options: {
    watch: { type: "boolean", help: t("commands.swiplet.service.watchState") },
  },
  run: setService(false),
});
