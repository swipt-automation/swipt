import fs from "node:fs";

import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { readStdin, uploadChunk } from "../../io";
import { session } from "../../session";
import { operandCount } from "./operand";

const writeFile = (target) => (context) =>
  session(context, async (client) => {
    const {
      id,
      args: [name, file],
    } = target(context.args);
    const data =
      file === undefined || file === "-" ? readStdin() : fs.readFileSync(file);
    await client.swiplets.writeDataFile(
      (async function* () {
        yield {
          part: { case: "header", value: { swipletId: id, path: name } },
        };
        for (let offset = 0; offset < data.length; offset += uploadChunk) {
          yield {
            part: {
              case: "data",
              value: data.subarray(offset, offset + uploadChunk),
            },
          };
        }
      })(),
      { signal: context.signal },
    );
  });

export const write = (operand, target) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "write",
    use: `write ${operand}PATH [FILE | -]`,
    short: tr("Write a data file"),
    args: [before + 1, before + 2],
    run: writeFile(target),
  });
};
