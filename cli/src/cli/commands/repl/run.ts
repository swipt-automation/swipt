import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { UsageError } from "../../../client";
import { tr } from "../../../i18n";
import { printScriptError } from "../../../output";
import { LogLevel } from "../../../proto/common_pb";
import { defineCommand } from "../../command";
import { readStdin } from "../../io";
import { session } from "../../session";

const isFile = (name) => {
  try {
    return fs.statSync(name).isFile();
  } catch {
    return false;
  }
};

const isProgram = (first) => first === "-" || isFile(first);

function readScript([first, ...args]) {
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
      tr("{argument} is not a file, and CODE takes no ARG", {
        argument: JSON.stringify(first),
      }),
    );
  }
  return { code: first, args, program: false, file: "input.js" };
}

class ImageSink {
  declare output: string | undefined;
  directory = "";

  constructor(output) {
    this.output = output;
  }

  save(data) {
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

async function runScript(context) {
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
  short: tr("Run a JavaScript expression or program on the device"),
  description: [
    tr(
      "The first argument is FILE when it names an existing file and CODE otherwise; options go before it.",
    ),
    tr(
      "CODE is an expression whose value is printed. FILE, or standard input given as - or no argument, is a program, and only its console output is printed.",
    ),
  ],
  args: [0, Infinity],
  passthrough: isProgram,
  options: {
    output: {
      type: "string",
      value: "DIRECTORY",
      help: tr("image directory (default: a new temporary directory)"),
    },
  },
  run: runScript,
});
