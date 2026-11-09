import { tr } from "../../../../i18n";
import { defineCommand } from "../../../command";
import { setService } from "./services";

export const disable = defineCommand({
  name: "disable",
  use: "disable ID",
  short: tr("Turn off a swiplet service"),
  args: [1, 1],
  options: {
    watch: { type: "boolean", help: tr("watch the state") },
  },
  run: setService(false),
});
