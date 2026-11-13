import { execFileSync } from "node:child_process";
import fs from "node:fs";

import i18next from "i18next";

const locales = new URL("../locales/", import.meta.url);

const languages = new Set(fs.readdirSync(locales));

function environmentLanguages(variables: NodeJS.ProcessEnv) {
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

function registryValue(key: string, name: string) {
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

function supportedLanguage(tag: string) {
  let locale;
  try {
    locale = new Intl.Locale(tag).maximize();
  } catch {
    return undefined;
  }
  const { language, script, region } = locale;
  const variants: Record<string, string> = {
    es: region === "ES" ? "es" : "es-419",
    no: "nb",
    pt: "pt-PT",
  };
  return [
    `${language}-${region}`,
    `${language}-${script}`,
    variants[language],
    language,
  ].find((candidate) => languages.has(candidate));
}

export const language =
  Iterator.from(environmentLanguages(process.env) ?? systemLanguages())
    .map((tag) => supportedLanguage(tag.split(/[.@]/)[0].replaceAll("_", "-")))
    .find(Boolean) ?? "en";

const resources = Object.fromEntries(
  [...new Set([language, "en"])].map((it) => [
    it,
    {
      translation: JSON.parse(
        fs.readFileSync(new URL(`${it}/translation.json`, locales), "utf8"),
      ),
    },
  ]),
);

i18next.init({
  lng: language,
  fallbackLng: "en",
  resources,
  initAsync: false,
  interpolation: { escapeValue: false },
});

export const t = i18next.getFixedT(language);
