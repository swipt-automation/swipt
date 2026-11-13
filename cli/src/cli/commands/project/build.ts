import path from "node:path";

import { t } from "../../../i18n";
import { buildProject } from "../../../project";
import { defineCommand } from "../../command";

export const build = defineCommand({
  name: "build",
  use: "build [PROJECT]",
  short: t("commands.project.build.description"),
  args: [0, 1],
  run: async ({ args }) => {
    const { archive } = await buildProject(path.resolve(args[0] ?? "."));
    console.log(path.relative(process.cwd(), archive));
  },
});
