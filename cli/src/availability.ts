import fs from "node:fs";
import path from "node:path";

import { NodeFlags, SyntaxKind } from "typescript/unstable/ast";
import { isTypeNode } from "typescript/unstable/ast/is";
import { API, SymbolFlags } from "typescript/unstable/sync";

import { language, tr } from "./i18n";
import { parseVersion, versionAtLeast } from "./version";

const requirements = {
  apiLevel: {
    parse: (text) => (/^[1-9]\d*$/.test(text) ? Number(text) : undefined),
    covers: (level, needed) => level >= needed,
    describe: (level) => tr("API level {level}", { level }),
  },
  iOS: {
    parse: parseVersion,
    covers: versionAtLeast,
    describe: (version) => `iOS ${version.join(".")}`,
  },
};

const lists = new Intl.ListFormat(language, { type: "conjunction" });

const merge = (into, from) => {
  for (const [name, needed] of Object.entries(from)) {
    if (
      into[name] === undefined ||
      !requirements[name].covers(into[name], needed)
    ) {
      into[name] = needed;
    }
  }
  return into;
};

const describe = (needed, missing) =>
  lists.format(
    missing.map((name) => requirements[name].describe(needed[name])),
  );

function parseAnnotation(text) {
  const requirement = {};
  for (const entry of text.split(",").map((part) => part.trim())) {
    const [kind, value, ...rest] = entry.split(/\s+/);
    const parsed =
      rest.length === 0 && Object.hasOwn(requirements, kind)
        ? requirements[kind].parse(value ?? "")
        : undefined;
    if (parsed === undefined) {
      return { requirement, invalid: entry };
    }
    if (requirement[kind] !== undefined) {
      return { requirement, repeated: kind };
    }
    requirement[kind] = parsed;
  }
  return { requirement };
}

const tagText = (tag) =>
  typeof tag.comment === "string"
    ? tag.comment
    : (tag.comment ?? []).map((part) => part.text).join("");

const annotationTags = (node) =>
  (node.jsDoc ?? []).flatMap((comment) =>
    (comment.tags ?? []).filter((tag) => tag.tagName.text === "available"),
  );

const exits = (statement) =>
  statement.kind === SyntaxKind.ReturnStatement ||
  statement.kind === SyntaxKind.ThrowStatement ||
  statement.kind === SyntaxKind.BreakStatement ||
  statement.kind === SyntaxKind.ContinueStatement ||
  (statement.kind === SyntaxKind.Block &&
    statement.statements.length > 0 &&
    exits(statement.statements.at(-1)));

const hasModifier = (node, kind) =>
  node.modifiers?.some((modifier) => modifier.kind === kind) ?? false;

const functions = new Set([
  SyntaxKind.FunctionDeclaration,
  SyntaxKind.FunctionExpression,
  SyntaxKind.ArrowFunction,
  SyntaxKind.MethodDeclaration,
  SyntaxKind.GetAccessor,
  SyntaxKind.SetAccessor,
  SyntaxKind.Constructor,
]);

const classes = new Set([
  SyntaxKind.ClassDeclaration,
  SyntaxKind.ClassExpression,
]);

const valueHolders = new Set([
  SyntaxKind.VariableDeclaration,
  SyntaxKind.PropertyDeclaration,
  SyntaxKind.PropertyAssignment,
]);

const objectMembers = new Set([
  SyntaxKind.PropertyAssignment,
  SyntaxKind.ShorthandPropertyAssignment,
  SyntaxKind.MethodDeclaration,
  SyntaxKind.GetAccessor,
  SyntaxKind.SetAccessor,
]);

const inert = new Set([
  SyntaxKind.ImportDeclaration,
  SyntaxKind.ExportDeclaration,
  SyntaxKind.InterfaceDeclaration,
  SyntaxKind.TypeAliasDeclaration,
  SyntaxKind.JsxClosingElement,
]);

function sourceFilesOf(root, metafiles) {
  const files = new Set();
  for (const metafile of metafiles) {
    for (const input of Object.keys(metafile.inputs)) {
      const file = path.resolve(input);
      if (
        !/^[a-z][\w-]+:/.test(input) &&
        /\.[cm]?[jt]sx?$/.test(file) &&
        !path.relative(root, file).startsWith("..") &&
        !file.split(path.sep).includes("node_modules")
      ) {
        files.add(fs.realpathSync(file));
      }
    }
  }
  return [...files];
}

export class AvailabilityChecker {
  #api;
  #open = new Set();

  check(project, metafiles) {
    const files = sourceFilesOf(project.root, metafiles);
    if (files.length === 0 && this.#open.size === 0) {
      return;
    }
    this.#api ??= new API({ cwd: project.root });
    // An open file keeps the content it was opened with, whatever fileChanges says
    // (typescript 7.0.2).
    if (this.#open.size > 0) {
      this.#api.updateSnapshot({ closeFiles: [...this.#open] });
    }
    const snapshot = this.#api.updateSnapshot({
      openFiles: files,
      fileChanges: { invalidateAll: true },
    });
    this.#open = new Set(files);
    if (files.length === 0) {
      return;
    }
    const problems = { lines: new Set(), guarding: false };
    const groups = Map.groupBy(files, (file) =>
      snapshot.getDefaultProjectForFile(file),
    );
    for (const [typescript, owned] of groups) {
      new Checker(typescript, project, files, problems).check(owned);
    }
    if (problems.lines.size > 0) {
      throw new Error(
        [
          ...problems.lines,
          ...(problems.guarding
            ? [
                tr(
                  'check these with available(), mark the code that uses them with @available, or raise "swiplet.minimumAPILevel" or "swiplet.minimumIOSVersion" in package.json',
                ),
              ]
            : []),
        ].join("\n"),
      );
    }
  }

  close() {
    this.#api?.close();
  }
}

class Checker {
  #project;
  #checker;
  #root;
  #files;
  #problems;
  #minimum;
  #entries;
  #available;
  #symbols = new Map();
  #requirements = new Map();
  #annotations = new Map();
  #facts = [new Map(), new Map()];
  #types = new Map();

  constructor(typescript, project, files, problems) {
    this.#project = typescript;
    this.#checker = typescript.checker;
    this.#root = project.root;
    this.#files = new Set(files);
    this.#problems = problems;
    this.#minimum = {
      apiLevel: project.config.minimumAPILevel ?? project.apiLevel,
      iOS: parseVersion(
        project.config.minimumIOSVersion ?? project.minimumIOSVersion,
      ),
    };
    this.#entries = new Set(
      [project.app, project.extension]
        .filter(Boolean)
        .map((entry) => fs.realpathSync(entry)),
    );
    this.#available = this.#availableSymbol(project.apiDirectory);
  }

  check(files) {
    for (const file of files) {
      const source = this.#project.program.getSourceFile(file);
      const { identifiers, properties } = this.#collect(source);
      const symbols = this.#checker.getSymbolAtLocation(identifiers);
      for (const [index, node] of identifiers.entries()) {
        this.#use(
          node,
          node.parent.kind === SyntaxKind.ShorthandPropertyAssignment
            ? this.#checker.getShorthandAssignmentValueSymbol(node.parent)
            : symbols[index],
        );
      }
      for (const node of properties) {
        this.#use(node, this.#propertySymbol(node));
      }
      if (this.#entries.has(file)) {
        for (const statement of source.statements) {
          if (
            statement.name !== undefined &&
            hasModifier(statement, SyntaxKind.DefaultKeyword)
          ) {
            this.#useEntry(statement.name);
          }
        }
      }
    }
  }

  #collect(source) {
    const identifiers = [];
    const properties = [];
    const visit = (node, checked) => {
      for (const tag of annotationTags(node)) {
        this.#parse(tagText(tag), tag);
      }
      const extended =
        node.kind === SyntaxKind.ExpressionWithTypeArguments &&
        node.parent.token === SyntaxKind.ExtendsKeyword &&
        classes.has(node.parent.parent.kind);
      if (inert.has(node.kind) || (isTypeNode(node) && !extended)) {
        checked = false;
      }
      if (checked && node.kind === SyntaxKind.Identifier) {
        const { parent } = node;
        const property =
          parent.kind === SyntaxKind.JsxAttribute ||
          (objectMembers.has(parent.kind) &&
            parent.parent.kind === SyntaxKind.ObjectLiteralExpression &&
            parent.name === node) ||
          (parent.kind === SyntaxKind.BindingElement &&
            parent.parent.kind === SyntaxKind.ObjectBindingPattern &&
            (parent.propertyName ?? parent.name) === node);
        if (property) {
          properties.push(node);
        }
        if (
          parent.kind === SyntaxKind.ShorthandPropertyAssignment ||
          (!property &&
            parent.propertyName !== node &&
            (parent.name !== node ||
              parent.kind === SyntaxKind.PropertyAccessExpression))
        ) {
          identifiers.push(node);
        }
      }
      node.forEachChild((child) => visit(child, checked));
    };
    source.forEachChild((child) => visit(child, true));
    return { identifiers, properties };
  }

  #use(node, symbol) {
    if (symbol === undefined) {
      return;
    }
    const needed = this.#requirementOf(symbol);
    const missing = Object.keys(needed).filter(
      (name) => !this.#covered(name, needed[name], node),
    );
    if (missing.length > 0) {
      this.#problems.guarding = true;
      this.#problems.lines.add(
        tr("{location}: {name} needs {requirement}", {
          location: this.#location(node),
          name: node.text,
          requirement: describe(needed, missing),
        }),
      );
    }
  }

  #useEntry(name) {
    const symbol = this.#checker.getSymbolAtLocation(name);
    const needed = symbol === undefined ? {} : this.#requirementOf(symbol);
    const missing = Object.keys(needed).filter(
      (kind) => !requirements[kind].covers(this.#minimum[kind], needed[kind]),
    );
    if (missing.length > 0) {
      this.#problems.lines.add(
        tr(
          '{location}: {name} is an entry of the swiplet and needs {requirement}; raise "swiplet.minimumAPILevel" or "swiplet.minimumIOSVersion" in package.json',
          {
            location: this.#location(name),
            name: name.text,
            requirement: describe(needed, missing),
          },
        ),
      );
    }
  }

  #location(node) {
    const source = sourceFileOf(node);
    const { line, character } = source.getLineAndCharacterOfPosition(
      node.getStart(source),
    );
    return `${path.relative(this.#root, source.fileName)}:${line + 1}:${character + 1}`;
  }

  #parse(text, node) {
    const { requirement, invalid, repeated } = parseAnnotation(text);
    if (invalid !== undefined) {
      this.#problems.lines.add(
        tr(
          '{location}: @available has "{entry}"; it takes "apiLevel <level>" and "iOS <version>", separated by commas',
          { location: this.#location(node), entry: invalid },
        ),
      );
    } else if (repeated !== undefined) {
      this.#problems.lines.add(
        tr("{location}: @available names {kind} twice", {
          location: this.#location(node),
          kind: repeated,
        }),
      );
    }
    return requirement;
  }

  #target(symbol) {
    return symbol.flags & SymbolFlags.Alias
      ? this.#checker.getAliasedSymbol(symbol)
      : symbol;
  }

  #symbolOf(node) {
    if (!this.#symbols.has(node)) {
      const symbol = this.#checker.getSymbolAtLocation(node);
      this.#symbols.set(
        node,
        symbol === undefined ? undefined : this.#target(symbol),
      );
    }
    return this.#symbols.get(node);
  }

  #requirementOf(symbol) {
    const target = this.#target(symbol);
    if (!this.#requirements.has(target.id)) {
      const requirement = {};
      const declaration = (
        target.valueDeclaration ?? target.declarations[0]
      )?.resolve(this.#project);
      const external =
        declaration !== undefined &&
        !this.#files.has(sourceFileOf(declaration).fileName);
      for (const tag of this.#checker.getJsDocTagsOfSymbol(target)) {
        if (tag.name === "available") {
          merge(
            requirement,
            external
              ? this.#parse(tag.text ?? "", declaration)
              : parseAnnotation(tag.text ?? "").requirement,
          );
        }
      }
      this.#requirements.set(target.id, requirement);
    }
    return this.#requirements.get(target.id);
  }

  #annotationOf(node) {
    if (!this.#annotations.has(node)) {
      const requirement = {};
      const symbol =
        node.name?.kind === SyntaxKind.Identifier
          ? this.#checker.getSymbolAtLocation(node.name)
          : undefined;
      if (symbol !== undefined) {
        merge(requirement, this.#requirementOf(symbol));
      }
      for (const tag of annotationTags(node)) {
        merge(requirement, parseAnnotation(tagText(tag)).requirement);
      }
      this.#annotations.set(node, requirement);
    }
    return this.#annotations.get(node);
  }

  #propertySymbol(name) {
    const member = name.parent;
    const container = member.parent;
    if (!this.#types.has(container)) {
      this.#types.set(
        container,
        member.kind === SyntaxKind.BindingElement
          ? this.#checker.getTypeAtLocation(container)
          : this.#checker.getContextualType(container),
      );
    }
    const type = this.#types.get(container);
    if (type === undefined) {
      return undefined;
    }
    for (const candidate of [type, ...(type.getTypes?.() ?? [])]) {
      const property = this.#checker.getPropertyOfType(candidate, name.text);
      if (property !== undefined) {
        return property;
      }
    }
    return undefined;
  }

  #covered(name, needed, reference) {
    const { covers } = requirements[name];
    if (covers(this.#minimum[name], needed)) {
      return true;
    }
    let crossed = false;
    for (
      let child = reference, parent = reference.parent;
      parent;
      child = parent, parent = parent.parent
    ) {
      const proven = this.#provenBy(parent, child, crossed)[name];
      if (proven !== undefined && covers(proven, needed)) {
        return true;
      }
      crossed ||= functions.has(parent.kind);
    }
    return false;
  }

  #provenBy(parent, child, crossed) {
    const proven = {};
    switch (parent.kind) {
      case SyntaxKind.IfStatement:
        if (child === parent.thenStatement) {
          merge(proven, this.#factsOf(parent.expression, true));
        } else if (child === parent.elseStatement) {
          merge(proven, this.#factsOf(parent.expression, false));
        }
        break;
      case SyntaxKind.ConditionalExpression:
        if (child === parent.whenTrue) {
          merge(proven, this.#factsOf(parent.condition, true));
        } else if (child === parent.whenFalse) {
          merge(proven, this.#factsOf(parent.condition, false));
        }
        break;
      case SyntaxKind.BinaryExpression:
        if (child === parent.right) {
          const operator = parent.operatorToken.kind;
          if (operator === SyntaxKind.AmpersandAmpersandToken) {
            merge(proven, this.#factsOf(parent.left, true));
          } else if (operator === SyntaxKind.BarBarToken) {
            merge(proven, this.#factsOf(parent.left, false));
          }
        }
        break;
    }
    for (const statement of parent.statements ?? []) {
      if (statement === child) {
        break;
      }
      if (
        statement.kind === SyntaxKind.IfStatement &&
        statement.elseStatement === undefined &&
        exits(statement.thenStatement)
      ) {
        merge(proven, this.#factsOf(statement.expression, false));
      }
    }
    if (
      functions.has(parent.kind) ||
      (classes.has(parent.kind) &&
        (crossed ||
          (child.kind === SyntaxKind.PropertyDeclaration &&
            !hasModifier(child, SyntaxKind.StaticKeyword)))) ||
      (valueHolders.has(parent.kind) && crossed && child === parent.initializer)
    ) {
      merge(proven, this.#annotationOf(parent));
    }
    return proven;
  }

  #factsOf(node, outcome) {
    const cache = this.#facts[Number(outcome)];
    if (!cache.has(node)) {
      cache.set(node, {});
      cache.set(node, this.#computeFacts(node, outcome));
    }
    return cache.get(node);
  }

  #computeFacts(node, outcome) {
    switch (node.kind) {
      case SyntaxKind.Identifier: {
        const value = this.#constant(node);
        return value === node ? {} : this.#factsOf(value, outcome);
      }
      case SyntaxKind.ParenthesizedExpression:
        return this.#factsOf(node.expression, outcome);
      case SyntaxKind.PrefixUnaryExpression:
        return node.operator === SyntaxKind.ExclamationToken
          ? this.#factsOf(node.operand, !outcome)
          : {};
      case SyntaxKind.CallExpression:
        return outcome && this.#isAvailable(node.expression)
          ? this.#requirementsOf(node.arguments[0])
          : {};
      case SyntaxKind.BinaryExpression: {
        const operator = node.operatorToken.kind;
        if (operator === SyntaxKind.AmpersandAmpersandToken) {
          return outcome
            ? merge(
                merge({}, this.#factsOf(node.left, true)),
                this.#factsOf(node.right, true),
              )
            : {};
        }
        if (operator === SyntaxKind.BarBarToken) {
          return outcome
            ? {}
            : merge(
                merge({}, this.#factsOf(node.left, false)),
                this.#factsOf(node.right, false),
              );
        }
        return {};
      }
    }
    return {};
  }

  #constant(node) {
    for (const seen = new Set(); node.kind === SyntaxKind.Identifier;) {
      const declaration = this.#symbolOf(node)?.valueDeclaration?.resolve(
        this.#project,
      );
      if (
        declaration?.kind !== SyntaxKind.VariableDeclaration ||
        declaration.initializer === undefined ||
        !(declaration.parent.flags & NodeFlags.Const) ||
        seen.has(declaration)
      ) {
        break;
      }
      seen.add(declaration);
      node = declaration.initializer;
    }
    return node;
  }

  #isAvailable(node) {
    const name =
      node.kind === SyntaxKind.PropertyAccessExpression ? node.name : node;
    return (
      this.#available !== undefined &&
      name.kind === SyntaxKind.Identifier &&
      this.#symbolOf(name)?.id === this.#available
    );
  }

  #requirementsOf(argument) {
    const proven = {};
    for (const property of argument?.kind === SyntaxKind.ObjectLiteralExpression
      ? argument.properties
      : []) {
      const value =
        property.kind === SyntaxKind.PropertyAssignment &&
        Object.hasOwn(requirements, property.name.text)
          ? this.#constant(property.initializer)
          : undefined;
      if (value?.kind === SyntaxKind.NumericLiteral) {
        const parsed = requirements[property.name.text].parse(
          String(Number(value.text)),
        );
        if (parsed !== undefined) {
          proven[property.name.text] = parsed;
        }
      }
    }
    return proven;
  }

  #availableSymbol(apiDirectory) {
    const source = this.#project.program.getSourceFile(
      fs.realpathSync(path.join(apiDirectory, "index.d.ts")),
    );
    const declaration = source?.statements.find(
      (statement) =>
        statement.kind === SyntaxKind.FunctionDeclaration &&
        statement.name?.text === "available",
    );
    return declaration === undefined
      ? undefined
      : this.#checker.getSymbolAtLocation(declaration.name)?.id;
  }
}

function sourceFileOf(node) {
  while (node.kind !== SyntaxKind.SourceFile) {
    node = node.parent;
  }
  return node;
}
