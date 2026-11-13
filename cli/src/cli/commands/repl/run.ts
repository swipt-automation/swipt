import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { UsageError } from "../../../client";
import { t } from "../../../i18n";
import { printScriptError } from "../../../output";
import { LogLevel } from "../../../proto/common_pb";
import { type Context, defineCommand } from "../../command";
import { readStdin } from "../../io";
import { session } from "../../session";

const isFile = (name: string) => {
  try {
    return fs.statSync(name).isFile();
  } catch {
    return false;
  }
};

const isProgram = (first: string) => first === "-" || isFile(first);

function readScript([first, ...args]: string[]) {
  if (first === undefined || first === "-") {
    return {
      code: readStdin().toString(),
      args,
      program: true,
      file: "input.js",
    };
  }
  if (isFile(first)) {
    return {
      code: fs.readFileSync(first, "utf8"),
      args,
      program: true,
      file: first,
    };
  }
  if (args.length > 0) {
    throw new UsageError(
      t("commands.repl.run.extraArguments", {
        argument: JSON.stringify(first),
      }),
    );
  }
  return { code: first, args, program: false, file: "input.js" };
}

class ImageSink {
  declare output: string | undefined;
  directory = "";

  constructor(output: string | undefined) {
    this.output = output;
  }

  save(data: Uint8Array) {
    if (this.directory === "") {
      if (this.output !== undefined && this.output !== "") {
        this.directory = path.resolve(this.output);
        fs.mkdirSync(this.directory, { recursive: true });
      } else {
        this.directory = fs.mkdtempSync(path.join(os.tmpdir(), "swipt-repl-"));
      }
    }
    const name = path.join(
      this.directory,
      `image-${crypto.randomBytes(5).toString("hex")}.png`,
    );
    fs.writeFileSync(name, data);
    return name;
  }
}

async function runScript(
  context: Context<{ device?: string; output?: string }>,
) {
  const { args, options, signal } = context;
  const script = readScript(args);
  await session(context, async (client) => {
    const images = new ImageSink(options.output);
    let failure;
    for await (const response of client.repl.execute(
      { code: script.code, args: script.args, program: script.program },
      { signal },
    )) {
      switch (response.part.case) {
        case "console": {
          const output = response.part.value;
          (output.level === LogLevel.ERROR || output.level === LogLevel.WARN
            ? console.error
            : console.log)(output.text);
          break;
        }
        case "value":
          console.log(response.part.value.text);
          break;
        case "image":
          console.log(`[image: ${images.save(response.part.value)}]`);
          break;
        case "error":
          failure = response.part.value;
          break;
      }
    }
    if (failure) {
      throw printScriptError({
        ...failure,
        stack: failure.stack.map((frame) =>
          frame.file === "input.js" ? { ...frame, file: script.file } : frame,
        ),
      });
    }
  });
}

export const run = defineCommand({
  name: "run",
  use: "run [CODE | FILE [ARG...] | - [ARG...]]",
  short: t("commands.repl.run.description"),
  description: [
    t("commands.repl.run.arguments"),
    t("commands.repl.run.details"),
  ],
  args: [0, Infinity],
  passthrough: isProgram,
  options: {
    output: {
      type: "string",
      value: "DIRECTORY",
      help: t("commands.repl.run.images"),
    },
  },
  run: runScript,
});
