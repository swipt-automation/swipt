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

import { language, tr } from "./i18n";
import type { Project } from "./project";
import { parseVersion, versionAtLeast } from "./version";
interface Requirements {
  apiLevel: number;
  iOS: readonly number[];
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
    describe: (level) => tr("API level {level}", { level }),
  },
  iOS: {
    parse: parseVersion,
    covers: versionAtLeast,
    describe: (version) => `iOS ${version.join(".")}`,
  },
};

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

type Coverage = <K extends Kind>(kind: K, needed: Requirements[K]) => boolean;

const unmet = (needed: Requirement, covered: Coverage) => {
  const missing = kindsOf(needed).flatMap(<K extends Kind>(kind: K) => {
    const value = needed[kind];
    return value === undefined || covered(kind, value)
      ? []
      : [requirements[kind].describe(value)];
  });
  return missing.length > 0 ? lists.format(missing) : undefined;
};

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

interface Problems {
  readonly lines: Set<string>;
  guarding: boolean;
}

export class AvailabilityChecker {
  #api: API | undefined;
  #open = new Set<string>();

  check(project: Project, metafiles: readonly Metafile[]) {
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
    const problems: Problems = { lines: new Set(), guarding: false };
    const groups = Map.groupBy(files, (file) => {
      const typescript = snapshot.getDefaultProjectForFile(file);
      if (typescript === undefined) {
        throw new Error(`no TypeScript project contains ${file}`);
      }
      return typescript;
    });
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
                  'Check these with available(), mark the code that uses them with @available, or raise "swiplet.minimumAPILevel" or "swiplet.minimumIOSVersion" in package.json.',
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
  #project: TypeScriptProject;
  #checker: TypeScriptChecker;
  #root: string;
  #files: Set<string>;
  #problems: Problems;
  #minimum: Requirement;
  #entries: Set<string>;
  #available: number | undefined;
  #symbols = new Map<Node, TypeScriptSymbol | undefined>();
  #imports = new Set<Node>();
  #requirements = new Map<number, Requirement>();
  #annotations = new Map<Node, Requirement>();
  #facts: [Map<Node, Requirement>, Map<Node, Requirement>] = [
    new Map(),
    new Map(),
  ];
  #types = new Map<Node, Type | undefined>();
  #annotated = new Map<number, TypeScriptSymbol[]>();

  constructor(
    typescript: TypeScriptProject,
    project: Project,
    files: readonly string[],
    problems: Problems,
  ) {
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
        .filter((entry) => entry !== undefined)
        .map((entry) => fs.realpathSync(entry)),
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
      if (
        !named.has(property.name) &&
        this.#checker.getPropertyOfType(type, property.name) !== undefined
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
    const requirement = unmet(this.#requirementOf(symbol), (kind, needed) =>
      this.#covered(kind, needed, node),
    );
    if (requirement !== undefined) {
      this.#problems.guarding = true;
      this.#problems.lines.add(
        tr("{location}: {name} needs {requirement}.", {
          location: this.#location(node),
          name,
          requirement,
        }),
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
    const requirement = unmet(this.#moduleRequirement(module), (kind, needed) =>
      this.#meetsMinimum(kind, needed),
    );
    if (requirement !== undefined) {
      this.#problems.lines.add(
        tr(
          '{location}: importing {name} needs {requirement}; raise "swiplet.minimumAPILevel" or "swiplet.minimumIOSVersion" in package.json.',
          {
            location: this.#location(name),
            name: name.text,
            requirement,
          },
        ),
      );
    }
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
    const requirement = unmet(
      symbol === undefined ? {} : this.#requirementOf(symbol),
      (kind, needed) => this.#meetsMinimum(kind, needed),
    );
    if (requirement !== undefined) {
      this.#problems.lines.add(
        tr(
          '{location}: {name} is an entry of the swiplet and needs {requirement}; raise "swiplet.minimumAPILevel" or "swiplet.minimumIOSVersion" in package.json.',
          {
            location: this.#location(name),
            name: name.text,
            requirement,
          },
        ),
      );
    }
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
      this.#problems.lines.add(
        tr(
          '{location}: @available has "{entry}"; it takes "apiLevel <level>" and "iOS <version>", separated by commas.',
          {
            location: this.#location(node),
            entry: invalid,
          },
        ),
      );
    } else if (repeated !== undefined) {
      this.#problems.lines.add(
        tr("{location}: @available names {kind} twice.", {
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
      if (external) {
        merge(requirement, this.#moduleRequirement(sourceFileOf(declaration)));
      }
      this.#requirements.set(target.id, requirement);
    }
    return requirement;
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
      const proven = this.#provenBy(parent, child, crossed)[kind];
      if (proven !== undefined && covers(proven, needed)) {
        return true;
      }
      crossed ||= isFunctionLikeDeclaration(parent);
    }
    return false;
  }

  #provenBy(parent: Node, child: Node, crossed: boolean) {
    const proven: Requirement = {};
    if (isIfStatement(parent)) {
      if (child === parent.thenStatement) {
        merge(proven, this.#factsOf(parent.expression, true));
      } else if (child === parent.elseStatement) {
        merge(proven, this.#factsOf(parent.expression, false));
      }
    } else if (isConditionalExpression(parent)) {
      if (child === parent.whenTrue) {
        merge(proven, this.#factsOf(parent.condition, true));
      } else if (child === parent.whenFalse) {
        merge(proven, this.#factsOf(parent.condition, false));
      }
    } else if (isBinaryExpression(parent) && child === parent.right) {
      const operator = parent.operatorToken.kind;
      if (operator === SyntaxKind.AmpersandAmpersandToken) {
        merge(proven, this.#factsOf(parent.left, true));
      } else if (operator === SyntaxKind.BarBarToken) {
        merge(proven, this.#factsOf(parent.left, false));
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
        merge(proven, this.#factsOf(statement.expression, false));
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
      merge(proven, this.#annotationOf(parent));
    }
    return proven;
  }

  #factsOf(node: Expression, outcome: boolean) {
    const cache = this.#facts[Number(outcome)];
    let facts = cache.get(node);
    if (facts === undefined) {
      cache.set(node, {});
      facts = this.#computeFacts(node, outcome);
      cache.set(node, facts);
    }
    return facts;
  }

  #computeFacts(node: Expression, outcome: boolean): Requirement {
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
      return outcome && this.#isAvailable(node.expression)
        ? this.#requirementsOf(node.arguments.at(0))
        : {};
    }
    if (isBinaryExpression(node)) {
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

  #requirementsOf(argument: Expression | undefined) {
    const proven: Requirement = {};
    for (const property of argument !== undefined &&
    isObjectLiteralExpression(argument)
      ? argument.properties
      : []) {
      if (
        !isPropertyAssignment(property) ||
        isComputedPropertyName(property.name)
      ) {
        continue;
      }
      const kind = property.name.text;
      if (!isKind(kind)) {
        continue;
      }
      const value = this.#constant(property.initializer);
      const parsed = isNumericLiteral(value)
        ? parseKind(kind, String(Number(value.text)))
        : undefined;
      if (parsed !== undefined) {
        Object.assign(proven, parsed);
      }
    }
    return proven;
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

function sourceFileOf(node: Node) {
  while (!isSourceFile(node)) {
    node = node.parent;
  }
  return node;
}
