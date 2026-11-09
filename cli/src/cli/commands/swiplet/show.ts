import { tr } from "../../../i18n";
import { serviceText } from "../../../output";
import { defineCommand } from "../../command";
import { session } from "../../session";

const indented = (value) =>
  JSON.stringify(value, null, 2).replaceAll("\n", "\n  ");

const notInstalled = (id) =>
  new Error(tr("there is no swiplet {id}", { id: JSON.stringify(id) }));

const showSwiplet = (context) =>
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
    if (swiplet.hasUi) {
      console.log(tr("ui: yes"));
    }
    if (swiplet.service) {
      console.log(
        tr("service: {status}", {
          status: serviceText(swiplet.id, swiplet.service),
        }),
      );
    }
    for (const action of swiplet.actions) {
      console.log();
      console.log(
        action.exposedToShortcuts
          ? tr("action {name} {title} (Shortcuts)", {
              name: action.name,
              title: JSON.stringify(action.title),
            })
          : tr("action {name} {title}", {
              name: action.name,
              title: JSON.stringify(action.title),
            }),
      );
      if (action.description !== "") {
        console.log(`  ${action.description}`);
      }
      if (action.inputSchema) {
        console.log(
          `  ${tr("input: {schema}", { schema: indented(action.inputSchema) })}`,
        );
      }
      if (action.outputSchema) {
        console.log(
          `  ${tr("output: {schema}", { schema: indented(action.outputSchema) })}`,
        );
      }
    }
  });

export const show = defineCommand({
  name: "show",
  use: "show ID",
  short: tr("Show a swiplet"),
  args: [1, 1],
  run: showSwiplet,
});
