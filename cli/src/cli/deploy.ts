import fs from "node:fs";

import { create } from "@bufbuild/protobuf";

import type { Client } from "../client";
import { t } from "../i18n";
import {
  type DeploySwipletResponse,
  SwipletSchema,
} from "../proto/swiplets_pb";
import { uploadChunk } from "./io";

export async function deploy(
  client: Client,
  archive: string,
  signal: AbortSignal,
) {
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

export const printDeployed = (
  { replaced, swiplet = create(SwipletSchema) }: DeploySwipletResponse,
  toStdout: boolean,
) =>
  (toStdout ? console.log : console.error)(
    replaced
      ? t("deploy.replaced", { id: swiplet.id, version: swiplet.version })
      : t("deploy.installed", { id: swiplet.id, version: swiplet.version }),
  );
