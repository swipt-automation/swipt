import fs from "node:fs";
import path from "node:path";

import { UsageError } from "../../../client";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { session } from "../../session";
import { operandCount } from "./operand";

const isLocal = (name) => {
  const normalized = path.posix.normalize(name);
  return (
    !path.posix.isAbsolute(normalized) &&
    normalized !== ".." &&
    !normalized.startsWith("../")
  );
};

class FileSink {
  declare root: string;
  files = new Map();
  saved = 0;

  constructor(root) {
    this.root = root;
  }

  target(name) {
    if (!isLocal(name)) {
      throw new Error(
        `the server sent the path ${name}, which leaves the output directory`,
      );
    }
    return path.join(this.root, ...name.split("/"));
  }

  begin(name, directory) {
    const target = this.target(name);
    if (directory) {
      fs.mkdirSync(target, { recursive: true });
      return;
    }
    fs.mkdirSync(path.dirname(target), { recursive: true });
    const previous = this.files.get(name);
    if (previous !== undefined) {
      fs.closeSync(previous);
    }
    this.files.set(name, fs.openSync(target, "w"));
    this.saved++;
  }

  write(name, offset, data) {
    const file = this.files.get(name);
    if (file === undefined) {
      throw new Error(`the server sent data for ${name} before announcing it`);
    }
    fs.writeSync(file, data, 0, data.length, Number(offset));
  }

  close() {
    for (const file of this.files.values()) {
      fs.closeSync(file);
    }
  }
}

const printSaved = (target, count) =>
  console.error(tr("saved {count} file(s) to {path}", { count, path: target }));

const readFile = (target) => (context) =>
  session(context, async (client) => {
    const {
      id,
      wholeDirectory,
      subdirectory,
      args: [given],
    } = target(context.args);
    const output = context.options.output ?? "";
    const requested = path.posix
      .normalize(`/${given}`)
      .replace(/^\/+|\/+$/g, "");
    const printing = output === "" || output === "-";
    const directory = () =>
      new UsageError(
        requested === ""
          ? wholeDirectory(JSON.stringify(given))
          : subdirectory(JSON.stringify(requested)),
      );
    const relativeTo = (name) =>
      name.slice(requested.length).replace(/^\//, "");
    let sink;
    let single;
    const contents = [];
    try {
      for await (const response of client.swiplets.readDataFile(
        { swipletId: id, path: given },
        { signal: context.signal },
      )) {
        if (response.part.case === "file") {
          const file = response.part.value;
          if (file.path === requested && !file.directory) {
            single = file;
            continue;
          }
          if (!sink) {
            if (printing) {
              throw directory();
            }
            sink = new FileSink(output);
          }
          sink.begin(relativeTo(file.path), file.directory);
        } else if (response.part.case === "chunk") {
          const chunk = response.part.value;
          if (single && chunk.path === single.path) {
            contents.push(chunk.data);
            continue;
          }
          if (!sink) {
            throw new Error(
              `the server sent data for ${chunk.path} before announcing it`,
            );
          }
          sink.write(relativeTo(chunk.path), chunk.offset, chunk.data);
        }
      }
    } finally {
      sink?.close();
    }
    if (sink) {
      printSaved(sink.root, sink.saved);
      return;
    }
    if (!single) {
      if (printing) {
        throw directory();
      }
      fs.mkdirSync(output, { recursive: true });
      printSaved(output, 0);
      return;
    }
    const data = Buffer.concat(contents);
    if (printing) {
      process.stdout.write(data);
      return;
    }
    let destination = output;
    if (fs.existsSync(destination) && fs.statSync(destination).isDirectory()) {
      destination = path.join(destination, path.posix.basename(single.path));
    }
    fs.writeFileSync(destination, data);
    printSaved(destination, 1);
  });

export const read = (operand, target) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "read",
    use: `read ${operand}PATH`,
    short: tr("Read a data file or directory"),
    args: [before + 1, before + 1],
    options: {
      output: { type: "string", value: "PATH", help: tr("destination") },
    },
    run: readFile(target),
  });
};
