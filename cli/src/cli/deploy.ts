import fs from "node:fs";

import { tr } from "../i18n";
import { uploadChunk } from "./io";

export async function deploy(client, archive, signal) {
  const data = fs.readFileSync(archive);
  return client.swiplets.deploySwiplet(
    (async function* () {
      for (
        let offset = 0;
        offset < data.length || offset === 0;
        offset += uploadChunk
      ) {
        yield { chunk: data.subarray(offset, offset + uploadChunk) };
      }
    })(),
    { signal },
  );
}

export const printDeployed = (deployed, toStdout) =>
  (toStdout ? console.log : console.error)(
    deployed.replaced
      ? tr("replaced {id} {version}", {
          id: deployed.swiplet.id,
          version: deployed.swiplet.version,
        })
      : tr("installed {id} {version}", {
          id: deployed.swiplet.id,
          version: deployed.swiplet.version,
        }),
  );
