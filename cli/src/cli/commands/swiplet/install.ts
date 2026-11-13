import fs from "node:fs";

import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { deploy, printDeployed } from "../../deploy";
import { session } from "../../session";

async function installArchive(context: Context<{ device?: string }>) {
  const [archive] = context.args;
  if (!fs.existsSync(archive)) {
    throw new Error(t("commands.swiplet.install.notFound", { path: archive }));
  }
  await session(context, async (client) =>
    printDeployed(await deploy(client, archive, context.signal), true),
  );
}

export const install = defineCommand({
  name: "install",
  use: "install ARCHIVE",
  short: t("commands.swiplet.install.description"),
  args: [1, 1],
  run: installArchive,
});
