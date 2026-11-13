import fs from "node:fs";

import { fromJson, toJson } from "@bufbuild/protobuf";
import { ValueSchema } from "@bufbuild/protobuf/wkt";

import { UsageError } from "../../../client";
import { t } from "../../../i18n";
import { errorMessage, printLog, printScriptError } from "../../../output";
import { type Context, defineCommand } from "../../command";
import { readStdin } from "../../io";
import { session } from "../../session";

function readInput(value: string | undefined) {
  if (value === undefined || value === "") {
    return undefined;
  }
  let data = value;
  try {
    if (value === "-") {
      data = readStdin().toString();
    } else if (value.startsWith("@")) {
      data = fs.readFileSync(value.slice(1), "utf8");
    }
  } catch (error) {
    throw new Error(`read --input: ${errorMessage(error)}`);
  }
  try {
    return fromJson(ValueSchema, JSON.parse(data));
  } catch (error) {
    throw new UsageError(
      t("commands.swiplet.call.invalidInput", {
        error: errorMessage(error),
      }),
    );
  }
}

const callAction = (context: Context<{ device?: string; input?: string }>) =>
  session(context, async (client) => {
    const [id, action] = context.args;
    const input = readInput(context.options.input);
    let result;
    for await (const response of client.swiplets.callAction(
      { swipletId: id, action, input },
      { signal: context.signal },
    )) {
      if (response.part.case === "log") {
        printLog(response.part.value, false);
      } else if (response.part.case === "value") {
        console.log(JSON.stringify(toJson(ValueSchema, response.part.value)));
      } else if (response.part.case === "result") {
        result = response.part.value;
      }
    }
    if (result?.outcome.case === "output") {
      console.log(
        JSON.stringify(toJson(ValueSchema, result.outcome.value), null, 2),
      );
      return;
    }
    if (result?.outcome.case === "error") {
      throw printScriptError(result.outcome.value);
    }
    if (result === undefined) {
      throw new Error("the server sent no result");
    }
  });

export const call = defineCommand({
  name: "call",
  use: "call ID ACTION",
  short: t("commands.swiplet.call.description"),
  args: [2, 2],
  options: {
    input: {
      type: "string",
      value: "JSON",
      help: t("commands.swiplet.call.input"),
    },
  },
  run: callAction,
});
