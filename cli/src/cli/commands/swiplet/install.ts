import fs from "node:fs";

import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { deploy, printDeployed } from "../../deploy";
import { session } from "../../session";

async function installArchive(context) {
  const [archive] = context.args;
  if (!fs.existsSync(archive)) {
    throw new Error(tr("there is no archive at {path}", { path: archive }));
  }
  await session(context, async (client) =>
    printDeployed(await deploy(client, archive, context.signal), true),
  );
}

export const install = defineCommand({
  name: "install",
  use: "install ARCHIVE",
  short: tr("Install a swiplet archive"),
  args: [1, 1],
  run: installArchive,
});
