import fs from "node:fs";
import path from "node:path";

import { UsageError } from "../../../client";
import { tr } from "../../../i18n";
import type { FileInfo } from "../../../proto/common_pb";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { type ResolveTarget, operandCount } from "./operand";

class FileSink {
  declare root: string;
  files = new Map<string, number>();
  saved = 0;

  constructor(root: string) {
    this.root = root;
  }

  target(name: string) {
    const target = path.resolve(this.root, ...name.split("/"));
    const relative = path.relative(path.resolve(this.root), target);
    if (
      relative === ".." ||
      relative.startsWith(`..${path.sep}`) ||
      path.isAbsolute(relative)
    ) {
      throw new Error(
        `the server sent the path ${name}, which leaves the output directory`,
      );
    }
    return target;
  }

  begin(name: string, directory: boolean) {
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

  write(name: string, offset: bigint, data: Uint8Array) {
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

const printSaved = (target: string, count: number) =>
  console.error(
    tr(
      {
        one: "saved {count} file to {path}",
        other: "saved {count} files to {path}",
      },
      { count, path: target },
    ),
  );

const readFile =
  (target: ResolveTarget) =>
  (context: Context<{ device?: string; output?: string }>) =>
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
      const relativeTo = (name: string) =>
        name.slice(requested.length).replace(/^\//, "");
      let sink: FileSink | undefined;
      let single: FileInfo | undefined;
      const contents: Uint8Array[] = [];
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
      if (
        fs.existsSync(destination) &&
        fs.statSync(destination).isDirectory()
      ) {
        destination = path.join(destination, path.posix.basename(single.path));
      }
      fs.writeFileSync(destination, data);
      printSaved(destination, 1);
    });

export const read = (operand: string, target: ResolveTarget) => {
  const before = operandCount(operand);
  return defineCommand({
    name: "read",
    use: `read ${operand}PATH`,
    short: tr("Read a data file or directory"),
    args: [before + 1, before + 1],
    options: {
      output: {
        type: "string",
        value: "PATH",
        help: tr("destination"),
      },
    },
    run: readFile(target),
  });
};
