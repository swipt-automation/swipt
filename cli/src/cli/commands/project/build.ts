import path from "node:path";

import { tr } from "../../../i18n";
import { buildProject } from "../../../project";
import { defineCommand } from "../../command";

export const build = defineCommand({
  name: "build",
  use: "build [PROJECT]",
  short: tr("Bundle the project into dist/<swiplet.id>.swiplet"),
  args: [0, 1],
  run: async ({ args }) => {
    const { archive } = await buildProject(path.resolve(args[0] ?? "."));
    console.log(path.relative(process.cwd(), archive));
  },
});
