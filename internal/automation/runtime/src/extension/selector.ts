import { errorFrom } from "./errors";
import { native } from "./native";

const relations = ["within", "parent", "has", "not"];

export interface Elements<Element> {
  refOf(value: unknown): string | undefined;
  predicateArgument(argument: string): Element;
  adopt(answer: unknown): Element;
}

export interface Encoded<Element> {
  value: unknown;
  predicates: ((element: Element) => unknown)[];
  patterns: RegExp[];
}

export const encode = <Element>(
  selector: unknown,
  refOf: (value: unknown) => string | undefined,
): Encoded<Element> => {
  const encoded: Encoded<Element> = {
    value: undefined,
    predicates: [],
    patterns: [],
  };
  const convert = (value: unknown): unknown => {
    if (typeof value === "function") {
      encoded.predicates.push(value as (element: Element) => unknown);
      return { $predicate: encoded.predicates.length - 1 };
    }
    if (
      value === null ||
      typeof value !== "object" ||
      Array.isArray(value) ||
      refOf(value) !== undefined
    ) {
      throw new TypeError(
        "expected a selector: an object of element fields, or a function of an element",
      );
    }
    const out: Record<string, unknown> = {};
    for (const [key, field] of Object.entries(value)) {
      if (field === undefined) {
        continue;
      }
      const ref = relations.includes(key) ? refOf(field) : undefined;
      if (field instanceof RegExp) {
        encoded.patterns.push(field);
        out[key] = {
          $regex: field.source,
          $flags: field.flags,
          $id: encoded.patterns.length - 1,
        };
      } else if (ref !== undefined) {
        out[key] = { $element: ref };
      } else if (relations.includes(key)) {
        out[key] = convert(field);
      } else {
        out[key] = field;
      }
    }
    return out;
  };
  encoded.value = convert(selector);
  return encoded;
};

export const search = <Element>(
  snapshot: string,
  scope: string,
  selector: unknown,
  mode: "one" | "all",
  elements: Elements<Element>,
): Element[] => {
  const { value, predicates, patterns } = encode<Element>(
    selector,
    elements.refOf,
  );
  let thrown: { error: unknown } | undefined;
  const answer = native.match(
    snapshot,
    scope,
    JSON.stringify(value),
    mode,
    (kind, id, argument) => {
      try {
        const matched =
          kind === "match"
            ? argument.search(patterns[Number(id)]) >= 0
            : Boolean(
                predicates[Number(id)](elements.predicateArgument(argument)),
              );
        return matched ? "true" : "false";
      } catch (error) {
        thrown = { error };
        return "throw";
      }
    },
  );
  if (thrown !== undefined) {
    throw thrown.error;
  }
  if (answer.failure !== undefined) {
    throw errorFrom(JSON.parse(answer.failure));
  }
  return (answer.elements ?? []).map(elements.adopt);
};
