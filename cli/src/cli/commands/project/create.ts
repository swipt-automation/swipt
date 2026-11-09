import { tr } from "../../../i18n";
import { createCommand } from "../../../project";
import { defineCommand } from "../../command";

export const create = defineCommand({
  name: "create",
  use: "create DIRECTORY",
  short: tr("Make a swiplet project in a new or empty directory"),
  args: [1, 1],
  run: ({ args }) => createCommand(args[0]),
});
