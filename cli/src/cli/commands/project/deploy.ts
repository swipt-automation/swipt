import path from "node:path";

import { tr } from "../../../i18n";
import { buildProject } from "../../../project";
import { defineCommand } from "../../command";
import { deploy as deploySwiplet, printDeployed } from "../../deploy";
import { session } from "../../session";
import { deviceOption } from "../flags";

async function deployProject(context) {
  const { archive } = await buildProject(path.resolve(context.args[0] ?? "."));
  await session(context, async (client) =>
    printDeployed(await deploySwiplet(client, archive, context.signal), true),
  );
}

export const deploy = defineCommand({
  name: "deploy",
  use: "deploy [PROJECT]",
  short: tr("Build the project and install it on the device"),
  args: [0, 1],
  options: deviceOption,
  run: deployProject,
});
