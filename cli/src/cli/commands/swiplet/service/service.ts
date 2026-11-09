import { tr } from "../../../../i18n";
import { defineCommand } from "../../../command";
import { disable } from "./disable";
import { enable } from "./enable";
import { watch } from "./watch";

export const service = defineCommand({
  name: "service",
  short: tr("Manage swiplet services"),
  commands: [enable, disable, watch],
});
