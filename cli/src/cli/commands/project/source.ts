import { tr } from "../../../i18n";
import { sourceCommand } from "../../../project";
import { defineCommand } from "../../command";

export const source = defineCommand({
  name: "source",
  use: "source [SWIPLET...]",
  short: tr("Collect the built swiplets into dist/ with a swiplets.json"),
  args: [0, Infinity],
  run: ({ args }) => sourceCommand(process.cwd(), args),
});
