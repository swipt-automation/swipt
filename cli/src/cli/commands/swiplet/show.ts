import type { JsonObject } from "@bufbuild/protobuf";

import { t } from "../../../i18n";
import { serviceText } from "../../../output";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";

const indented = (value: JsonObject) =>
  JSON.stringify(value, null, 2).replaceAll("\n", "\n  ");

const notInstalled = (id: string) =>
  new Error(t("commands.swiplet.show.notFound", { id: JSON.stringify(id) }));

const showSwiplet = (context: Context<{ device?: string }>) =>
  session(context, async (client) => {
    const { swiplets } = await client.swiplets.listSwiplets(
      {},
      { signal: context.signal },
    );
    const swiplet = swiplets.find((it) => it.id === context.args[0]);
    if (!swiplet) {
      throw notInstalled(context.args[0]);
    }
    console.log(swiplet.id, JSON.stringify(swiplet.title), swiplet.version);
    if (swiplet.service) {
      console.log(
        t("commands.swiplet.show.service", {
          status: serviceText(swiplet.service),
        }),
      );
    }
    for (const action of swiplet.actions) {
      console.log();
      console.log(action.name, JSON.stringify(action.title));
      if (action.description !== "") {
        console.log(`  ${action.description}`);
      }
      if (action.inputSchema) {
        console.log(
          `  ${t("commands.swiplet.show.input", { schema: indented(action.inputSchema) })}`,
        );
      }
      if (action.outputSchema) {
        console.log(
          `  ${t("commands.swiplet.show.output", { schema: indented(action.outputSchema) })}`,
        );
      }
    }
  });

export const show = defineCommand({
  name: "show",
  use: "show ID",
  short: t("commands.swiplet.show.description"),
  args: [1, 1],
  run: showSwiplet,
});
