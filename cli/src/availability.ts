import fs from "node:fs";
import path from "node:path";

import type { Metafile } from "esbuild";
import {
  type ClassLikeDeclaration,
  type ExportDeclaration,
  type Expression,
  type FunctionLikeDeclaration,
  type Identifier,
  type ImportDeclaration,
  type JSDocTag,
  type ModifiersBase,
  type Node,
  NodeFlags,
  type ObjectLiteralElementLike,
  type PropertyAssignment,
  type PropertyDeclaration,
  type SourceFile,
  type Statement,
  type StringLiteralLikeNode,
  SyntaxKind,
  type VariableDeclaration,
} from "typescript/unstable/ast";
import {
  isAccessorDeclaration,
  isArrowFunction,
  isBinaryExpression,
  isBindingElement,
  isBlock,
  isBreakOrContinueStatement,
  isCallExpression,
  isCallOrNewExpression,
  isCaseClause,
  isClassDeclaration,
  isClassLikeDeclaration,
  isComputedPropertyName,
  isConditionalExpression,
  isConstructorDeclaration,
  isDefaultClause,
  isElementAccessExpression,
  isExportDeclaration,
  isExpressionWithTypeArguments,
  isFunctionDeclaration,
  isFunctionLikeDeclaration,
  isHeritageClause,
  isIdentifier,
  isIfStatement,
  isImportDeclaration,
  isInterfaceDeclaration,
  isJSDoc,
  isJsxAttribute,
  isJsxAttributes,
  isJsxSpreadAttribute,
  isMethodDeclaration,
  isMethodSignatureDeclaration,
  isModuleBlock,
  isNamedExports,
  isNumericLiteral,
  isNumericOrStringLikeLiteral,
  isObjectBindingPattern,
  isObjectLiteralExpression,
  isParenthesizedExpression,
  isPrefixUnaryExpression,
  isPropertyAccessExpression,
  isPropertyAssignment,
  isPropertyDeclaration,
  isReturnStatement,
  isShorthandPropertyAssignment,
  isSourceFile,
  isSpreadAssignment,
  isStringLiteralLikeNode,
  isThrowStatement,
  isTypeNode,
  isVariableDeclaration,
} from "typescript/unstable/ast/is";
import {
  API,
  SymbolFlags,
  type Checker as TypeScriptChecker,
  type Project as TypeScriptProject,
  type Symbol as TypeScriptSymbol,
  type Type,
} from "typescript/unstable/sync";

import { language, t } from "./i18n";
import { type OS, type Platform, systems } from "./platform";
import type { Build, Project } from "./project";
import { parseVersion, versionAtLeast } from "./version";

interface Requirements {
  apiLevel: number;
  iOS: readonly number[];
  android: number;
}

type Kind = keyof Requirements;

type Requirement = Partial<Requirements>;

interface Rule<Value> {
  readonly parse: (text: string) => Value | undefined;
  readonly covers: (value: Value, needed: Value) => boolean;
  readonly describe: (value: Value) => string;
}

const requirements: { readonly [K in Kind]: Rule<Requirements[K]> } = {
  apiLevel: {
    parse: (text) => (/^[1-9]\d*$/.test(text) ? Number(text) : undefined),
    covers: (level, needed) => level >= needed,
    describe: (level) => t("availability.apiLevel", { level }),
  },
  iOS: {
    parse: (text) => (text === "" ? [] : parseVersion(text)),
    covers: versionAtLeast,
    describe: (version) =>
      version.length === 0 ? "iOS" : `iOS ${version.join(".")}`,
  },
  android: {
    parse: (text) =>
      text === "" ? 0 : /^[1-9]\d*$/.test(text) ? Number(text) : undefined,
    covers: (sdk, needed) => sdk >= needed,
    describe: (sdk) => (sdk === 0 ? "Android" : `Android SDK ${sdk}`),
  },
};

const platformKinds: { readonly [Name in OS]: Kind } = {
  ios: "iOS",
  android: "android",
};

const fields: { readonly [K in Kind]: string } = {
  apiLevel: "swiplet.minimumAPILevel",
  iOS: "swiplet.platforms.ios.minimumVersion",
  android: "swiplet.platforms.android.minimumSDK",
};

const minimumOf = (project: Project, platform: Platform): Requirement =>
  platform.os === "ios"
    ? { apiLevel: project.minimumAPILevel, iOS: platform.minimumVersion }
    : { apiLevel: project.minimumAPILevel, android: platform.minimumSDK };

const isKind = (name: string): name is Kind =>
  Object.hasOwn(requirements, name);

const kindsOf = (requirement: Requirement) =>
  Object.keys(requirement).filter(isKind);

const parseKind = <K extends Kind>(
  kind: K,
  text: string,
): Requirement | undefined => {
  const value = requirements[kind].parse(text);
  if (value === undefined) {
    return undefined;
  }
  const requirement: Requirement = {};
  requirement[kind] = value;
  return requirement;
};

const lists = new Intl.ListFormat(language, { type: "conjunction" });

const mergeKind = <K extends Kind>(
  into: Requirement,
  from: Requirement,
  kind: K,
) => {
  const needed = from[kind];
  const current = into[kind];
  if (
    needed !== undefined &&
    (current === undefined || !requirements[kind].covers(current, needed))
  ) {
    into[kind] = needed;
  }
};

const merge = (into: Requirement, from: Requirement) => {
  for (const kind of kindsOf(from)) {
    mergeKind(into, from, kind);
  }
  return into;
};

const weakestKind = <K extends Kind>(
  into: Requirement,
  all: readonly Requirement[],
  kind: K,
) => {
  let weakest: Requirements[K] | undefined;
  for (const requirement of all) {
    const value = requirement[kind];
    if (value === undefined) {
      return;
    }
    if (weakest === undefined || requirements[kind].covers(weakest, value)) {
      weakest = value;
    }
  }
  if (weakest !== undefined) {
    into[kind] = weakest;
  }
};

const common = (all: readonly Requirement[]) => {
  const requirement: Requirement = {};
  for (const kind of kindsOf(all[0] ?? {})) {
    weakestKind(requirement, all, kind);
  }
  return requirement;
};

const unreachable = Symbol("unreachable");

type Facts = Requirement | typeof unreachable;

const both = (a: Facts, b: Facts): Facts =>
  a === unreachable || b === unreachable ? unreachable : merge(merge({}, a), b);

const either = (a: Facts, b: Facts): Facts =>
  a === unreachable ? b : b === unreachable ? a : common([a, b]);

interface Guard {
  readonly requirement: Requirement;
  readonly impossible: boolean;
  readonly certain: boolean;
}

type Shortfall = {
  readonly location: string;
  readonly name: string;
  readonly requirement: string;
  readonly fields: string;
};

type Coverage = <K extends Kind>(kind: K, needed: Requirements[K]) => boolean;

const unmetKinds = (needed: Requirement, covered: Coverage) =>
  kindsOf(needed).filter(<K extends Kind>(kind: K) => {
    const value = needed[kind];
    return value !== undefined && !covered(kind, value);
  });

const describe = (needed: Requirement, kinds: readonly Kind[]) =>
  lists.format(
    kinds.map(<K extends Kind>(kind: K) =>
      requirements[kind].describe(needed[kind] as Requirements[K]),
    ),
  );

interface Annotation {
  readonly requirement: Requirement;
  readonly invalid?: string;
  readonly repeated?: Kind;
}

function parseAnnotation(text: string): Annotation {
  const requirement: Requirement = {};
  for (const entry of text.split(",").map((part) => part.trim())) {
    const [kind = "", value = "", ...rest] = entry.split(/\s+/);
    const parsed =
      rest.length === 0 && isKind(kind) ? parseKind(kind, value) : undefined;
    if (parsed === undefined) {
      return { requirement, invalid: entry };
    }
    const repeated = kindsOf(parsed).find(
      (name) => requirement[name] !== undefined,
    );
    if (repeated !== undefined) {
      return { requirement, repeated };
    }
    Object.assign(requirement, parsed);
  }
  return { requirement };
}

const tagText = (tag: JSDocTag) =>
  (tag.comment ?? []).map((part) => part.text).join("");

const annotationTags = (node: Node) =>
  (node.jsDoc ?? [])
    .filter(isJSDoc)
    .flatMap((comment) =>
      (comment.tags ?? []).filter((tag) => tag.tagName.text === "available"),
    );

const exits = (statement: Statement): boolean => {
  if (
    isReturnStatement(statement) ||
    isThrowStatement(statement) ||
    isBreakOrContinueStatement(statement)
  ) {
    return true;
  }
  const last = isBlock(statement) ? statement.statements.at(-1) : undefined;
  return last !== undefined && exits(last);
};

const hasModifier = (node: ModifiersBase, kind: SyntaxKind) =>
  node.modifiers?.some((modifier) => modifier.kind === kind) ?? false;

const statementsOf = (node: Node): readonly Statement[] =>
  isBlock(node) ||
  isSourceFile(node) ||
  isModuleBlock(node) ||
  isCaseClause(node) ||
  isDefaultClause(node)
    ? node.statements
    : [];

const isNameOf = (node: Node) =>
  "name" in node.parent && node.parent.name === node;

const isPropertyNameOf = (node: Node) =>
  "propertyName" in node.parent && node.parent.propertyName === node;

const isObjectMember = (node: Node) =>
  isPropertyAssignment(node) ||
  isShorthandPropertyAssignment(node) ||
  isMethodDeclaration(node) ||
  isAccessorDeclaration(node);

const isValueHolder = (node: Node) =>
  isVariableDeclaration(node) ||
  isPropertyDeclaration(node) ||
  isPropertyAssignment(node);

type Annotated =
  | FunctionLikeDeclaration
  | ClassLikeDeclaration
  | VariableDeclaration
  | PropertyDeclaration
  | PropertyAssignment;

const writtenName = (member: ObjectLiteralElementLike) => {
  if (isSpreadAssignment(member)) {
    return undefined;
  }
  const { name } = member;
  if (isComputedPropertyName(name)) {
    return isNumericOrStringLikeLiteral(name.expression)
      ? name.expression.text
      : undefined;
  }
  return name.text;
};

const constituentsOf = (type: Type) =>
  type.isUnionType() ||
  type.isIntersectionType() ||
  type.isTemplateLiteralType()
    ? type.getTypes()
    : [];

const literals = new Set([
  SyntaxKind.ObjectLiteralExpression,
  SyntaxKind.ArrayLiteralExpression,
  SyntaxKind.ArrowFunction,
  SyntaxKind.FunctionExpression,
  SyntaxKind.StringLiteral,
  SyntaxKind.NumericLiteral,
  SyntaxKind.NoSubstitutionTemplateLiteral,
  SyntaxKind.TemplateExpression,
  SyntaxKind.TrueKeyword,
  SyntaxKind.FalseKeyword,
  SyntaxKind.NullKeyword,
  SyntaxKind.SpreadElement,
]);

const inert = new Set([
  SyntaxKind.ImportDeclaration,
  SyntaxKind.ExportDeclaration,
  SyntaxKind.InterfaceDeclaration,
  SyntaxKind.TypeAliasDeclaration,
  SyntaxKind.JsxClosingElement,
]);

function sourceFilesOf(root: string, metafiles: readonly Metafile[]) {
  const files = new Set<string>();
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

const configOf = (root: string, files: readonly string[], os: OS) => {
  const base = path.join(root, "tsconfig.json");
  const inherited = fs.existsSync(base);
  return {
    ...(inherited ? { extends: base } : {}),
    compilerOptions: {
      ...(inherited
        ? {}
        : {
            allowJs: true,
            jsx: "react-jsx",
            module: "esnext",
            moduleResolution: "bundler",
            target: "esnext",
            skipLibCheck: true,
          }),
      moduleSuffixes: [`.${os}`, ".native", ""],
    },
    files,
    include: [],
  };
};

export class AvailabilityChecker {
  #api: API | undefined;
  #open: string[] = [];

  check(project: Project, builds: readonly Build[]) {
    const targets = builds.map((build) => ({
      build,
      files: sourceFilesOf(project.root, build.metafiles),
      config: path.join(
        project.root,
        "dist",
        `tsconfig.${build.platform.os}.json`,
      ),
    }));
    const files = [...new Set(targets.flatMap((target) => target.files))];
    this.#api ??= new API({ cwd: project.root });
    if (this.#open.length > 0) {
      this.#api.updateSnapshot({ closeProjects: this.#open });
    }
    this.#open = [];
    if (files.length === 0) {
      return;
    }
    fs.mkdirSync(path.join(project.root, "dist"), { recursive: true });
    for (const { build, files: included, config } of targets) {
      fs.writeFileSync(
        config,
        `${JSON.stringify(configOf(project.root, included, build.platform.os), null, 2)}\n`,
      );
    }
    this.#open = targets.map((target) => target.config);
    const snapshot = this.#api.updateSnapshot({
      openProjects: this.#open,
      fileChanges: { invalidateAll: true },
    });
    const problems = new Set<string>();
    for (const { build, files: included, config } of targets) {
      const typescript = snapshot.getProject(config);
      if (typescript === undefined) {
        throw new Error(`TypeScript opened no project for ${config}`);
      }
      new Checker(typescript, project, build, files, problems).check(included);
    }
    if (problems.size > 0) {
      throw new Error([...problems].join("\n"));
    }
  }

  close() {
    this.#api?.close();
  }
}

class Checker {
  #project: TypeScriptProject;
  #checker: TypeScriptChecker;
  #root: string;
  #files: Set<string>;
  #problems: Set<string>;
  #minimum: Requirement;
  #platform: OS;
  #entries: Set<string>;
  #available: number | undefined;
  #symbols = new Map<Node, TypeScriptSymbol | undefined>();
  #imports = new Set<Node>();
  #requirements = new Map<number, Requirement>();
  #overloads = new Map<number, readonly Node[]>();
  #annotations = new Map<Node, Requirement>();
  #facts: [Map<Node, Facts>, Map<Node, Facts>] = [new Map(), new Map()];
  #types = new Map<Node, Type | undefined>();
  #annotated = new Map<number, TypeScriptSymbol[]>();

  constructor(
    typescript: TypeScriptProject,
    project: Project,
    build: Build,
    files: readonly string[],
    problems: Set<string>,
  ) {
    this.#project = typescript;
    this.#checker = typescript.checker;
    this.#root = project.root;
    this.#files = new Set(files);
    this.#problems = problems;
    this.#minimum = minimumOf(project, build.platform);
    this.#platform = build.platform.os;
    this.#entries = new Set(
      build.entries.map((entry) => fs.realpathSync(entry)),
    );
    this.#available = this.#availableSymbol(project.apiDirectory);
  }

  check(files: readonly string[]) {
    for (const file of files) {
      const source = this.#project.program.getSourceFile(file);
      if (source === undefined) {
        throw new Error(`the TypeScript program has no ${file}`);
      }
      const { identifiers, properties, values } = this.#collect(source);
      const symbols = this.#checker.getSymbolAtLocation(identifiers);
      for (const [index, node] of identifiers.entries()) {
        this.#use(
          node,
          isShorthandPropertyAssignment(node.parent)
            ? this.#checker.getShorthandAssignmentValueSymbol(node.parent)
            : symbols[index],
          node.text,
        );
      }
      for (const node of properties) {
        this.#use(node, this.#propertySymbol(node), node.text);
      }
      for (const node of values) {
        this.#useValue(node);
      }
      for (const statement of source.statements) {
        if (!isExportDeclaration(statement) || statement.isTypeOnly) {
          continue;
        }
        const { exportClause } = statement;
        const elements =
          exportClause !== undefined && isNamedExports(exportClause)
            ? exportClause.elements
            : undefined;
        const exported = (elements ?? []).filter(
          (specifier) => !specifier.isTypeOnly,
        );
        if (statement.moduleSpecifier === undefined) {
          for (const specifier of exported) {
            const local =
              this.#checker.getExportSpecifierLocalTargetSymbol(specifier);
            if (local !== undefined) {
              this.#useImport(local);
            }
          }
        } else if (exported.length > 0 || elements === undefined) {
          this.#importModule(statement);
        }
      }
      if (this.#entries.has(file)) {
        for (const statement of source.statements) {
          if (
            (isFunctionDeclaration(statement) ||
              isClassDeclaration(statement) ||
              isInterfaceDeclaration(statement)) &&
            statement.name !== undefined &&
            hasModifier(statement, SyntaxKind.DefaultKeyword)
          ) {
            this.#useEntry(statement.name);
          }
        }
      }
    }
  }

  #collect(source: SourceFile) {
    const identifiers: (Identifier | StringLiteralLikeNode)[] = [];
    const properties: (Identifier | StringLiteralLikeNode)[] = [];
    const values: Expression[] = [];
    const visit = (node: Node, checked: boolean) => {
      for (const tag of annotationTags(node)) {
        this.#parse(tagText(tag), tag);
      }
      const extended =
        isExpressionWithTypeArguments(node) &&
        isHeritageClause(node.parent) &&
        node.parent.token === SyntaxKind.ExtendsKeyword &&
        isClassLikeDeclaration(node.parent.parent);
      if (inert.has(node.kind) || (isTypeNode(node) && !extended)) {
        checked = false;
      }
      if (checked) {
        if (isCallOrNewExpression(node)) {
          values.push(
            ...(node.arguments ?? []).filter(
              (argument) => !literals.has(argument.kind),
            ),
          );
        } else if (isSpreadAssignment(node) || isJsxSpreadAttribute(node)) {
          values.push(node.expression);
        }
      }
      if (checked && isIdentifier(node)) {
        const { parent } = node;
        const property =
          isJsxAttribute(parent) ||
          (isObjectMember(parent) &&
            isObjectLiteralExpression(parent.parent) &&
            parent.name === node) ||
          (isBindingElement(parent) &&
            isObjectBindingPattern(parent.parent) &&
            (parent.propertyName ?? parent.name) === node);
        if (property) {
          properties.push(node);
        }
        if (
          isShorthandPropertyAssignment(parent) ||
          (!property &&
            !isPropertyNameOf(node) &&
            (!isNameOf(node) || isPropertyAccessExpression(parent)))
        ) {
          identifiers.push(node);
        }
      }
      if (checked && isStringLiteralLikeNode(node)) {
        const { parent } = node;
        const member = isComputedPropertyName(parent) ? parent.parent : parent;
        if (
          (isObjectMember(member) &&
            isObjectLiteralExpression(member.parent) &&
            (member.name === node || member.name === parent)) ||
          (isBindingElement(member) &&
            isObjectBindingPattern(member.parent) &&
            (member.propertyName === node || member.propertyName === parent))
        ) {
          properties.push(node);
        } else if (
          isElementAccessExpression(parent) &&
          parent.argumentExpression === node
        ) {
          identifiers.push(node);
        }
      }
      if (
        checked &&
        isObjectLiteralExpression(node) &&
        node.properties.some(
          (member) =>
            !isSpreadAssignment(member) &&
            isComputedPropertyName(member.name) &&
            !isStringLiteralLikeNode(member.name.expression),
        )
      ) {
        values.push(node);
      }
      node.forEachChild((child) => visit(child, checked));
    };
    source.forEachChild((child) => visit(child, true));
    return { identifiers, properties, values };
  }

  #useValue(node: Expression) {
    const expected = this.#checker.getContextualType(node);
    if (expected === undefined) {
      return;
    }
    let annotated = this.#annotated.get(expected.id);
    if (annotated === undefined) {
      const found = new Map<string, TypeScriptSymbol>();
      for (const candidate of [expected, ...constituentsOf(expected)]) {
        for (const property of this.#checker.getPropertiesOfType(candidate)) {
          if (Object.keys(this.#requirementOf(property)).length > 0) {
            found.set(property.name, property);
          }
        }
      }
      annotated = [...found.values()];
      this.#annotated.set(expected.id, annotated);
    }
    const type =
      annotated.length > 0 ? this.#checker.getTypeAtLocation(node) : undefined;
    if (type === undefined) {
      return;
    }
    const named = new Set(
      (isObjectLiteralExpression(node) ? node.properties : []).map(writtenName),
    );
    for (const property of annotated) {
      const own = this.#checker.getPropertyOfType(type, property.name);
      if (
        !named.has(property.name) &&
        own !== undefined &&
        this.#target(own).id !== this.#target(property).id
      ) {
        this.#use(node, property, property.name);
      }
    }
  }

  #use(node: Node, symbol: TypeScriptSymbol | undefined, name: string) {
    if (symbol === undefined) {
      return;
    }
    this.#useImport(symbol);
    const needed =
      this.#callRequirement(node, symbol) ?? this.#requirementOf(symbol);
    const covered: Coverage = (kind, value) => this.#covered(kind, value, node);
    const foreign = this.#foreignKinds(needed);
    if (foreign.length > 0) {
      if (
        unmetKinds(needed, covered).filter((kind) => foreign.includes(kind))
          .length === foreign.length
      ) {
        this.#problems.add(
          t("availability.foreignUse", this.#foreign(needed, node, name)),
        );
      }
      return;
    }
    const kinds = unmetKinds(needed, covered).filter((kind) =>
      this.#relevant(kind),
    );
    if (kinds.length > 0) {
      this.#problems.add(
        t("availability.needs", this.#shortfall(needed, kinds, node, name)),
      );
    }
  }

  #useImport(symbol: TypeScriptSymbol) {
    let declaration =
      symbol.flags & SymbolFlags.Alias
        ? symbol.declarations.at(0)?.resolve(this.#project)
        : undefined;
    while (declaration !== undefined && !isImportDeclaration(declaration)) {
      declaration = isSourceFile(declaration) ? undefined : declaration.parent;
    }
    if (declaration !== undefined) {
      this.#importModule(declaration);
    }
  }

  #importModule(declaration: ImportDeclaration | ExportDeclaration) {
    const name = declaration.moduleSpecifier;
    if (
      this.#imports.has(declaration) ||
      name === undefined ||
      !isStringLiteralLikeNode(name)
    ) {
      return;
    }
    this.#imports.add(declaration);
    const module = this.#checker
      .getSymbolAtLocation(name)
      ?.declarations.at(0)
      ?.resolve(this.#project);
    if (
      module === undefined ||
      !isSourceFile(module) ||
      this.#files.has(module.fileName)
    ) {
      return;
    }
    this.#requireMinimum(
      this.#moduleRequirement(module),
      name,
      name.text,
      (options) => t("availability.importNeeds", options),
    );
  }

  #moduleRequirement(source: SourceFile) {
    const requirement: Requirement = {};
    const first = source.statements.at(0);
    if (first !== undefined && isImportDeclaration(first)) {
      for (const tag of annotationTags(first)) {
        merge(requirement, this.#parse(tagText(tag), tag));
      }
    }
    return requirement;
  }

  #useEntry(name: Identifier) {
    const symbol = this.#checker.getSymbolAtLocation(name);
    this.#requireMinimum(
      symbol === undefined ? {} : this.#requirementOf(symbol),
      name,
      name.text,
      (options) => t("availability.entryNeeds", options),
    );
  }

  #requireMinimum(
    needed: Requirement,
    node: Node,
    name: string,
    message: (options: Shortfall) => string,
  ) {
    if (this.#foreignKinds(needed).length > 0) {
      this.#problems.add(
        t("availability.foreign", this.#foreign(needed, node, name)),
      );
      return;
    }
    const kinds = unmetKinds(needed, (kind, value) =>
      this.#meetsMinimum(kind, value),
    ).filter((kind) => this.#relevant(kind));
    if (kinds.length > 0) {
      this.#problems.add(message(this.#shortfall(needed, kinds, node, name)));
    }
  }

  #shortfall(
    needed: Requirement,
    kinds: readonly Kind[],
    node: Node,
    name: string,
  ): Shortfall {
    return {
      location: this.#location(node),
      name,
      requirement: describe(needed, kinds),
      fields: lists.format(kinds.map((kind) => `"${fields[kind]}"`)),
    };
  }

  #foreignKinds(needed: Requirement) {
    const named = Object.values(platformKinds).filter(
      (kind) => needed[kind] !== undefined,
    );
    return named.includes(platformKinds[this.#platform]) ? [] : named;
  }

  #relevant(kind: Kind) {
    return kind === "apiLevel" || kind === platformKinds[this.#platform];
  }

  #foreign(needed: Requirement, node: Node, name: string) {
    const platforms = (Object.keys(platformKinds) as OS[]).filter(
      (os) => needed[platformKinds[os]] !== undefined,
    );
    return {
      location: this.#location(node),
      name,
      systems: lists.format(platforms.map((os) => systems[os])),
      build: systems[this.#platform],
      os: platforms[0],
    };
  }

  #location(node: Node) {
    const source = sourceFileOf(node);
    const { line, character } = source.getLineAndCharacterOfPosition(
      node.getStart(source),
    );
    return `${path.relative(this.#root, source.fileName)}:${line + 1}:${character + 1}`;
  }

  #parse(text: string, node: Node) {
    const { requirement, invalid, repeated } = parseAnnotation(text);
    if (invalid !== undefined) {
      this.#problems.add(
        t("availability.invalidEntry", {
          location: this.#location(node),
          entry: invalid,
        }),
      );
    } else if (repeated !== undefined) {
      this.#problems.add(
        t("availability.repeatedEntry", {
          location: this.#location(node),
          kind: repeated,
        }),
      );
    }
    return requirement;
  }

  #target(symbol: TypeScriptSymbol) {
    return symbol.flags & SymbolFlags.Alias
      ? this.#checker.getAliasedSymbol(symbol)
      : symbol;
  }

  #symbolOf(node: Node) {
    if (!this.#symbols.has(node)) {
      const symbol = this.#checker.getSymbolAtLocation(node);
      this.#symbols.set(
        node,
        symbol === undefined ? undefined : this.#target(symbol),
      );
    }
    return this.#symbols.get(node);
  }

  #requirementOf(symbol: TypeScriptSymbol) {
    const target = this.#target(symbol);
    let requirement = this.#requirements.get(target.id);
    if (requirement === undefined) {
      requirement = {};
      const declaration = (
        target.valueDeclaration ?? target.declarations.at(0)
      )?.resolve(this.#project);
      const external =
        declaration !== undefined &&
        !this.#files.has(sourceFileOf(declaration).fileName);
      const overloads = this.#overloadsOf(target);
      if (overloads.length > 1) {
        merge(
          requirement,
          common(overloads.map((overload) => this.#declared(overload))),
        );
      } else {
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
      }
      if (external) {
        merge(requirement, this.#moduleRequirement(sourceFileOf(declaration)));
      }
      this.#requirements.set(target.id, requirement);
    }
    return requirement;
  }

  #overloadsOf(symbol: TypeScriptSymbol) {
    let overloads = this.#overloads.get(symbol.id);
    if (overloads === undefined) {
      overloads =
        symbol.declarations.length < 2
          ? []
          : symbol.declarations
              .map((declaration) => declaration.resolve(this.#project))
              .filter((declaration) => declaration !== undefined)
              .filter(
                (declaration) =>
                  isFunctionDeclaration(declaration) ||
                  isMethodSignatureDeclaration(declaration) ||
                  isMethodDeclaration(declaration),
              );
      this.#overloads.set(symbol.id, overloads);
    }
    return overloads;
  }

  #declared(declaration: Node) {
    const external = !this.#files.has(sourceFileOf(declaration).fileName);
    const requirement: Requirement = {};
    for (const tag of annotationTags(declaration)) {
      merge(
        requirement,
        external
          ? this.#parse(tagText(tag), tag)
          : parseAnnotation(tagText(tag)).requirement,
      );
    }
    return requirement;
  }

  #callRequirement(node: Node, symbol: TypeScriptSymbol) {
    const callee =
      isPropertyAccessExpression(node.parent) && node.parent.name === node
        ? node.parent
        : node;
    const call = callee.parent;
    if (
      !isCallOrNewExpression(call) ||
      call.expression !== callee ||
      this.#overloadsOf(this.#target(symbol)).length < 2
    ) {
      return undefined;
    }
    const declaration = this.#checker
      .getResolvedSignature(call)
      ?.declaration?.resolve(this.#project);
    return declaration === undefined
      ? undefined
      : merge(
          merge({}, this.#requirementOf(symbol)),
          this.#declared(declaration),
        );
  }

  #annotationOf(node: Annotated) {
    let requirement = this.#annotations.get(node);
    if (requirement === undefined) {
      requirement = {};
      const name =
        isArrowFunction(node) || isConstructorDeclaration(node)
          ? undefined
          : node.name;
      const symbol =
        name !== undefined && isIdentifier(name)
          ? this.#checker.getSymbolAtLocation(name)
          : undefined;
      if (symbol !== undefined) {
        merge(requirement, this.#requirementOf(symbol));
      }
      for (const tag of annotationTags(node)) {
        merge(requirement, parseAnnotation(tagText(tag)).requirement);
      }
      this.#annotations.set(node, requirement);
    }
    return requirement;
  }

  #propertySymbol(name: Identifier | StringLiteralLikeNode) {
    const member = isComputedPropertyName(name.parent)
      ? name.parent.parent
      : name.parent;
    const container = member.parent;
    if (!this.#types.has(container)) {
      this.#types.set(
        container,
        isObjectBindingPattern(container)
          ? this.#checker.getTypeAtLocation(container)
          : isObjectLiteralExpression(container) || isJsxAttributes(container)
            ? this.#checker.getContextualType(container)
            : undefined,
      );
    }
    const type = this.#types.get(container);
    if (type === undefined) {
      return undefined;
    }
    for (const candidate of [type, ...constituentsOf(type)]) {
      const property = this.#checker.getPropertyOfType(candidate, name.text);
      if (property !== undefined) {
        return property;
      }
    }
    return undefined;
  }

  #meetsMinimum<K extends Kind>(kind: K, needed: Requirements[K]) {
    const minimum = this.#minimum[kind];
    return minimum !== undefined && requirements[kind].covers(minimum, needed);
  }

  #covered<K extends Kind>(kind: K, needed: Requirements[K], reference: Node) {
    if (this.#meetsMinimum(kind, needed)) {
      return true;
    }
    const { covers } = requirements[kind];
    let crossed = false;
    for (let child = reference; !isSourceFile(child); child = child.parent) {
      const { parent } = child;
      const proven = this.#provenBy(parent, child, crossed);
      if (proven === unreachable) {
        return true;
      }
      const value = proven[kind];
      if (value !== undefined && covers(value, needed)) {
        return true;
      }
      crossed ||= isFunctionLikeDeclaration(parent);
    }
    return false;
  }

  #provenBy(parent: Node, child: Node, crossed: boolean) {
    let proven: Facts = {};
    if (isIfStatement(parent)) {
      if (child === parent.thenStatement) {
        proven = this.#factsOf(parent.expression, true);
      } else if (child === parent.elseStatement) {
        proven = this.#factsOf(parent.expression, false);
      }
    } else if (isConditionalExpression(parent)) {
      if (child === parent.whenTrue) {
        proven = this.#factsOf(parent.condition, true);
      } else if (child === parent.whenFalse) {
        proven = this.#factsOf(parent.condition, false);
      }
    } else if (isBinaryExpression(parent) && child === parent.right) {
      const operator = parent.operatorToken.kind;
      if (operator === SyntaxKind.AmpersandAmpersandToken) {
        proven = this.#factsOf(parent.left, true);
      } else if (operator === SyntaxKind.BarBarToken) {
        proven = this.#factsOf(parent.left, false);
      }
    }
    for (const statement of statementsOf(parent)) {
      if (statement === child) {
        break;
      }
      if (
        isIfStatement(statement) &&
        statement.elseStatement === undefined &&
        exits(statement.thenStatement)
      ) {
        proven = both(proven, this.#factsOf(statement.expression, false));
      }
    }
    if (
      isFunctionLikeDeclaration(parent) ||
      (isClassLikeDeclaration(parent) &&
        (crossed ||
          (isPropertyDeclaration(child) &&
            !hasModifier(child, SyntaxKind.StaticKeyword)))) ||
      (isValueHolder(parent) && crossed && child === parent.initializer)
    ) {
      proven = both(proven, this.#annotationOf(parent));
    }
    return proven;
  }

  #factsOf(node: Expression, outcome: boolean): Facts {
    const cache = this.#facts[Number(outcome)];
    let facts = cache.get(node);
    if (facts === undefined) {
      cache.set(node, {});
      facts = this.#computeFacts(node, outcome);
      cache.set(node, facts);
    }
    return facts;
  }

  #computeFacts(node: Expression, outcome: boolean): Facts {
    if (isIdentifier(node)) {
      const value = this.#constant(node);
      return value === node ? {} : this.#factsOf(value, outcome);
    }
    if (isParenthesizedExpression(node)) {
      return this.#factsOf(node.expression, outcome);
    }
    if (isPrefixUnaryExpression(node)) {
      return node.operator === SyntaxKind.ExclamationToken
        ? this.#factsOf(node.operand, !outcome)
        : {};
    }
    if (isCallExpression(node)) {
      if (!this.#isAvailable(node.expression)) {
        return {};
      }
      const { requirement, impossible, certain } = this.#guardOf(
        node.arguments.at(0),
      );
      if (outcome) {
        return impossible ? unreachable : requirement;
      }
      return certain &&
        !impossible &&
        unmetKinds(requirement, (kind, value) =>
          this.#meetsMinimum(kind, value),
        ).length === 0
        ? unreachable
        : {};
    }
    if (isBinaryExpression(node)) {
      const operator = node.operatorToken.kind;
      if (operator === SyntaxKind.AmpersandAmpersandToken) {
        return outcome
          ? both(
              this.#factsOf(node.left, true),
              this.#factsOf(node.right, true),
            )
          : either(
              this.#factsOf(node.left, false),
              both(
                this.#factsOf(node.left, true),
                this.#factsOf(node.right, false),
              ),
            );
      }
      if (operator === SyntaxKind.BarBarToken) {
        return outcome
          ? either(
              this.#factsOf(node.left, true),
              both(
                this.#factsOf(node.left, false),
                this.#factsOf(node.right, true),
              ),
            )
          : both(
              this.#factsOf(node.left, false),
              this.#factsOf(node.right, false),
            );
      }
      const negated =
        operator === SyntaxKind.ExclamationEqualsEqualsToken ||
        operator === SyntaxKind.ExclamationEqualsToken;
      if (
        negated ||
        operator === SyntaxKind.EqualsEqualsEqualsToken ||
        operator === SyntaxKind.EqualsEqualsToken
      ) {
        const left = this.#constant(node.left);
        const right = this.#constant(node.right);
        const [system, literal] = isSystemReference(left)
          ? [left, right]
          : [right, left];
        if (isSystemReference(system) && isStringLiteralLikeNode(literal)) {
          const actual = (literal.text === this.#platform) !== negated;
          return actual === outcome ? {} : unreachable;
        }
      }
    }
    return {};
  }

  #constant(node: Expression) {
    for (const seen = new Set<Node>(); isIdentifier(node);) {
      const declaration = this.#symbolOf(node)?.valueDeclaration?.resolve(
        this.#project,
      );
      if (
        declaration === undefined ||
        !isVariableDeclaration(declaration) ||
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

  #isAvailable(node: Expression) {
    const name = isPropertyAccessExpression(node) ? node.name : node;
    return (
      this.#available !== undefined &&
      isIdentifier(name) &&
      this.#symbolOf(name)?.id === this.#available
    );
  }

  #guardOf(argument: Expression | undefined): Guard {
    const requirement: Requirement = {};
    const own = platformKinds[this.#platform];
    let certain = argument !== undefined && isObjectLiteralExpression(argument);
    let named = false;
    let ownUnknown = false;
    for (const property of argument !== undefined &&
    isObjectLiteralExpression(argument)
      ? argument.properties
      : []) {
      if (
        !isPropertyAssignment(property) ||
        isComputedPropertyName(property.name)
      ) {
        certain = false;
        continue;
      }
      const kind = property.name.text;
      if (!isKind(kind)) {
        certain = false;
        continue;
      }
      const value = this.#constant(property.initializer);
      const parsed = isNumericLiteral(value)
        ? parseKind(kind, String(Number(value.text)))
        : value.kind === SyntaxKind.TrueKeyword
          ? parseKind(kind, "")
          : undefined;
      if (parsed === undefined) {
        certain = false;
        ownUnknown ||= kind === own;
        continue;
      }
      if (kind !== "apiLevel") {
        named = true;
        if (kind !== own) {
          continue;
        }
      }
      Object.assign(requirement, parsed);
    }
    return {
      requirement,
      impossible: named && requirement[own] === undefined && !ownUnknown,
      certain,
    };
  }

  #availableSymbol(apiDirectory: string) {
    const source = this.#project.program.getSourceFile(
      fs.realpathSync(path.join(apiDirectory, "index.d.ts")),
    );
    const name = source?.statements
      .filter(isFunctionDeclaration)
      .map((statement) => statement.name)
      .find((name) => name?.text === "available");
    return name === undefined
      ? undefined
      : this.#checker.getSymbolAtLocation(name)?.id;
  }
}

const isSystemReference = (node: Expression) =>
  isPropertyAccessExpression(node) &&
  isIdentifier(node.name) &&
  ((node.name.text === "EXPO_OS" &&
    isPropertyAccessExpression(node.expression) &&
    isIdentifier(node.expression.name) &&
    node.expression.name.text === "env" &&
    isIdentifier(node.expression.expression) &&
    node.expression.expression.text === "process") ||
    (node.name.text === "OS" &&
      isIdentifier(node.expression) &&
      node.expression.text === "Platform"));

function sourceFileOf(node: Node) {
  while (!isSourceFile(node)) {
    node = node.parent;
  }
  return node;
}
