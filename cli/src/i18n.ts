import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const locales = new URL("../locales/", import.meta.url);

const languages = new Set([
  "en",
  ...fs.readdirSync(locales).map((file) => path.basename(file, ".json")),
]);

function environmentLanguages(variables) {
  const locale = variables.LC_ALL || variables.LC_MESSAGES || variables.LANG;
  if (!locale) {
    return undefined;
  }
  if (/^(?:C|POSIX)(?:[.@]|$)/.test(locale)) {
    return [];
  }
  return [...(variables.LANGUAGE ?? "").split(":"), locale].filter(Boolean);
}

function appleLanguages() {
  try {
    return (
      execFileSync("defaults", ["read", "-g", "AppleLanguages"], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      }).match(/[A-Za-z][\w-]*/g) ?? []
    );
  } catch {
    return [];
  }
}

function registryValue(key, name) {
  let output;
  try {
    output = execFileSync("reg", ["query", key, "/v", name], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
  } catch {
    return undefined;
  }
  return /^\s+\S+\s+REG_MULTI_SZ\s+(.*?)\s*$/m.exec(output)?.[1].split("\\0");
}

function* windowsLanguages() {
  yield* registryValue(
    "HKEY_CURRENT_USER\\Control Panel\\Desktop",
    "PreferredUILanguages",
  ) ??
    registryValue(
      "HKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\Control\\MUI\\Settings",
      "PreferredUILanguages",
    ) ??
    [];
  yield* registryValue(
    "HKEY_CURRENT_USER\\Control Panel\\International\\User Profile",
    "Languages",
  ) ?? [];
}

function systemLanguages(): Iterable<string> {
  switch (process.platform) {
    case "win32":
      return windowsLanguages();
    case "darwin":
      return appleLanguages();
    default:
      return [];
  }
}

function supportedLanguage(tag) {
  let locale;
  try {
    locale = new Intl.Locale(tag).maximize();
  } catch {
    return undefined;
  }
  const { language, script, region } = locale;
  const variant = {
    es: region === "ES" ? "es" : "es-419",
    no: "nb",
    pt: "pt-PT",
  }[language];
  return [
    `${language}-${region}`,
    `${language}-${script}`,
    variant,
    language,
  ].find((candidate) => languages.has(candidate));
}

export const language =
  Iterator.from(environmentLanguages(process.env) ?? systemLanguages())
    .map((tag) => supportedLanguage(tag.split(/[.@]/)[0].replaceAll("_", "-")))
    .find(Boolean) ?? "en";

const messages =
  language === "en"
    ? {}
    : JSON.parse(fs.readFileSync(new URL(`${language}.json`, locales), "utf8"));

const pluralRules = new Intl.PluralRules(language);

export function tr(
  source: string | { one: string; other: string },
  values: Record<string, any> = {},
) {
  const message =
    messages[typeof source === "string" ? source : source.other] ?? source;
  const text =
    typeof message === "string"
      ? message
      : (message[pluralRules.select(values.count)] ?? message.other);
  return text.replace(/\{(\w+)\}/g, (_, name) => values[name]);
}
