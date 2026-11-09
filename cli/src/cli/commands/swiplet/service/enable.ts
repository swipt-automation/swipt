import { tr } from "../../../../i18n";
import { defineCommand } from "../../../command";
import { setService } from "./services";

export const enable = defineCommand({
  name: "enable",
  use: "enable ID",
  short: tr("Turn on a swiplet service"),
  args: [1, 1],
  options: {
    watch: { type: "boolean", help: tr("watch the state") },
  },
  run: setService(true),
});
