import path from "node:path";

import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { deviceOption } from "../flags";

async function deployProject(context: Context<{ device?: string }>) {
  const [{ buildProject }, { deploy: deploySwiplet, printDeployed }] =
    await Promise.all([import("../../../project"), import("../../deploy")]);
  const { archive } = await buildProject(path.resolve(context.args[0] ?? "."));
  await session(context, async (client) =>
    printDeployed(await deploySwiplet(client, archive, context.signal), true),
  );
}

export const deploy = defineCommand({
  name: "deploy",
  use: "deploy [PROJECT]",
  short: t("commands.project.deploy.description"),
  args: [0, 1],
  options: deviceOption,
  run: deployProject,
});
