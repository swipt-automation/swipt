import fs from "node:fs";
import path from "node:path";

const excluded = (name) =>
  name.startsWith(".") || name === "node_modules" || name === "dist";

const escape = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

interface Rule {
  negated: boolean;
  directory: boolean;
  pattern?: RegExp;
}

function parseRule(line) {
  line = line.replace(/[ \t\r]+$/, "");
  if (line === "" || line.startsWith("#")) {
    return undefined;
  }
  const rule: Rule = { negated: false, directory: false };
  if (line.startsWith("!")) {
    rule.negated = true;
    line = line.slice(1);
  } else if (line.startsWith("\\")) {
    line = line.slice(1);
  }
  if (line.endsWith("/")) {
    rule.directory = true;
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
    rule.pattern = new RegExp(`${expression}$`);
  } catch {
    return undefined;
  }
  return rule;
}

export const relativePath = (root, name) => {
  const relative = path.relative(root, name);
  return relative === "" ||
    relative.startsWith("..") ||
    path.isAbsolute(relative)
    ? ""
    : relative.split(path.sep).join("/");
};

export class Ignorer {
  rules = [];

  constructor(root) {
    const walk = (directory) => {
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

  read(name, base) {
    let text;
    try {
      text = fs.readFileSync(name, "utf8");
    } catch {
      return;
    }
    for (const line of text.split("\n")) {
      const rule = parseRule(line);
      if (rule) {
        this.rules.push({ ...rule, base });
      }
    }
  }

  ignored(relative, directory) {
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

  matches(relative, directory) {
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
