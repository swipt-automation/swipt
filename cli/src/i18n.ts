import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const locales = new URL("../locales/", import.meta.url);

const languages = new Set([
  "en",
  ...fs.readdirSync(locales).map((file) => path.basename(file, ".json")),
]);

function preferredLanguages() {
  const variable =
    process.env.LC_ALL || process.env.LC_MESSAGES || process.env.LANG;
  if (variable) {
    return [variable.split(/[.@]/)[0].replaceAll("_", "-")];
  }
  if (process.platform !== "darwin") {
    return [];
  }
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
  preferredLanguages().map(supportedLanguage).find(Boolean) ?? "en";

const messages =
  language === "en"
    ? {}
    : JSON.parse(fs.readFileSync(new URL(`${language}.json`, locales), "utf8"));

const pluralRules = new Intl.PluralRules(language);

export function tr(key, values: Record<string, any> = {}) {
  const message = messages[key] ?? key;
  const text =
    typeof message === "string"
      ? message
      : (message[pluralRules.select(values.count)] ?? message.other);
  return text.replace(/\{(\w+)\}/g, (_, name) => values[name]);
}
