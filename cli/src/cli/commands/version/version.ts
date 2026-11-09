import { createRequire } from "node:module";

import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

const { version: packageVersion } = createRequire(import.meta.url)(
  "../../../../package.json",
);

function printVersion() {
  console.log(
    "swipt",
    packageVersion,
    `node ${process.versions.node}`,
    `${process.platform}/${process.arch}`,
  );
}

export const version = defineCommand({
  name: "version",
  short: tr("Print the version"),
  args: [0, 0],
  run: printVersion,
});
