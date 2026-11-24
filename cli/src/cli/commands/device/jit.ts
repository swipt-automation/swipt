import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { deviceOption } from "../flags";

const enableJIT = (context: Context<{ device?: string }>) =>
  session(context, async (client) => {
    const [bundleId] = context.args;
    await client.device.enableJIT({ bundleId }, { signal: context.signal });
    console.log(t("commands.device.jit.enabled", { bundleId }));
  });

export const jit = defineCommand({
  name: "jit",
  use: "jit BUNDLE_ID",
  short: t("commands.device.jit.description"),
  args: [1, 1],
  options: deviceOption,
  run: enableJIT,
});
