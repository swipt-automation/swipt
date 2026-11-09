import { tr } from "../../../i18n";
import { columns, dateTime } from "../../../output";
import { defineCommand } from "../../command";
import { session } from "../../session";
import { operandCount } from "./operand";

const listFiles = (target) => (context) =>
  session(context, async (client) => {
    const {
      id,
      args: [directory = ""],
    } = target(context.args);
    const { files } = await client.swiplets.listDataFiles(
      {
        swipletId: id,
        path: directory,
        recursive: context.options.recursive,
      },
      { signal: context.signal },
    );
    const rows = files.map((file) => [
      file.directory ? "-" : String(file.size),
      dateTime(file.modified),
      file.directory ? `${file.path}/` : file.path,
    ]);
    if (rows.length > 0) {
      console.log(columns(rows));
    }
  });

export const list = (operand, target) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "list",
    use: `list ${operand}[PATH]`,
    short: tr("List data files"),
    args: [before, before + 1],
    options: {
      recursive: { type: "boolean", help: tr("include subdirectories") },
    },
    run: listFiles(target),
  });
};
