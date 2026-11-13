import fs from "node:fs";
import path from "node:path";

const excluded = (name: string) =>
  name.startsWith(".") || name === "node_modules" || name === "dist";

const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

interface Rule {
  negated: boolean;
  directory: boolean;
  pattern: RegExp;
  base: string;
}

function parseRule(line: string, base: string): Rule | undefined {
  line = line.replace(/[ \t\r]+$/, "");
  if (line === "" || line.startsWith("#")) {
    return undefined;
  }
  let negated = false;
  let directory = false;
  if (line.startsWith("!")) {
    negated = true;
    line = line.slice(1);
  } else if (line.startsWith("\\")) {
    line = line.slice(1);
  }
  if (line.endsWith("/")) {
    directory = true;
    line = line.replace(/\/+$/, "");
  }
  if (line === "") {
    return undefined;
  }
  const anchored = line.includes("/");
  line = line.replace(/^\//, "");
  let expression = anchored ? "^" : "^(?:.*/)?";
  for (let index = 0; index < line.length; index++) {
    const rest = line.slice(index);
    if (rest.startsWith("**/")) {
      expression += "(?:.*/)?";
      index += 2;
    } else if (rest === "/**") {
      expression += "/.*";
      index += 2;
    } else if (rest.startsWith("**")) {
      expression += ".*";
      index++;
    } else if (rest[0] === "*") {
      expression += "[^/]*";
    } else if (rest[0] === "?") {
      expression += "[^/]";
    } else if (rest[0] === "[" && rest.indexOf("]", 1) > 1) {
      const end = rest.indexOf("]", 1);
      let cls = rest.slice(1, end);
      if (cls.startsWith("!")) {
        cls = `^${cls.slice(1)}`;
      }
      expression += `[${cls.replaceAll("\\", "\\\\")}]`;
      index += end;
    } else if (rest[0] === "\\" && rest.length > 1) {
      expression += escape(rest[1]);
      index++;
    } else {
      expression += escape(rest[0]);
    }
  }
  try {
    return {
      negated,
      directory,
      pattern: new RegExp(`${expression}$`),
      base,
    };
  } catch {
    return undefined;
  }
}

export const relativePath = (root: string, name: string) => {
  const relative = path.relative(root, name);
  return relative === "" ||
    relative.startsWith("..") ||
    path.isAbsolute(relative)
    ? ""
    : relative.split(path.sep).join("/");
};

export class Ignorer {
  rules: Rule[] = [];

  constructor(root: string) {
    const walk = (directory: string) => {
      const relative = relativePath(root, directory);
      if (relative !== "" && this.ignored(relative, true)) {
        return;
      }
      this.read(path.join(directory, ".gitignore"), relative);
      let entries;
      try {
        entries = fs.readdirSync(directory, { withFileTypes: true });
      } catch {
        return;
      }
      for (const entry of entries) {
        if (entry.isDirectory()) {
          walk(path.join(directory, entry.name));
        }
      }
    };
    walk(root);
  }

  read(name: string, base: string) {
    let text;
    try {
      text = fs.readFileSync(name, "utf8");
    } catch {
      return;
    }
    for (const line of text.split("\n")) {
      const rule = parseRule(line, base);
      if (rule) {
        this.rules.push(rule);
      }
    }
  }

  ignored(relative: string, directory: boolean) {
    const parts = relative.split("/");
    return parts.some(
      (part, index) =>
        excluded(part) ||
        this.matches(
          parts.slice(0, index + 1).join("/"),
          directory || index < parts.length - 1,
        ),
    );
  }

  matches(relative: string, directory: boolean) {
    let ignored = false;
    for (const rule of this.rules) {
      if (rule.directory && !directory) {
        continue;
      }
      let local = relative;
      if (rule.base !== "") {
        if (!relative.startsWith(`${rule.base}/`)) {
          continue;
        }
        local = relative.slice(rule.base.length + 1);
      }
      if (rule.pattern.test(local)) {
        ignored = !rule.negated;
      }
    }
    return ignored;
  }
}
