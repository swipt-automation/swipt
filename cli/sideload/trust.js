const [encodedMessages, id, mode, unlock, launchError, promptMode] = args;
const autoAllow = promptMode !== "no-prompts";
const messages = JSON.parse(encodedMessages);
const filled = (message, value) => message.replace(/\{\w+\}/, () => value);
const name = "Swipt";
const settings = "com.apple.Preferences";
const springboard = "com.apple.springboard";
const developerPattern =
  /(?:Apple Development|iPhone Developer): .+?\([A-Z0-9]{10}\)/;

const escaped = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const sheet = (timeoutMs, signal) =>
  accessibility.find(
    { app: springboard, role: "sheet", label: /./ },
    { timeoutMs, signal },
  );

const sheetGone = (label, timeoutMs, signal) =>
  accessibility.find(
    {
      app: springboard,
      role: "application",
      not: { has: { role: "sheet", label } },
    },
    { timeoutMs, signal },
  );

async function matchingSheet(accept, timeoutMs, signal) {
  const until = Date.now() + (timeoutMs ?? 0);
  for (;;) {
    const remaining = Math.max(until - Date.now(), 0);
    const alert = await sheet(remaining, signal);
    if (alert === null) {
      return null;
    }
    if (accept(alert)) {
      return alert;
    }
    const left = until - Date.now();
    if (left <= 0) {
      return null;
    }
    if ((await sheetGone(alert.label, left, signal)) === null) {
      return null;
    }
  }
}

const promptSheet = (timeoutMs, signal) =>
  matchingSheet(
    (alert) =>
      Boolean(alert.label?.includes(name)) &&
      [2, 3].includes(alert.findAll({ role: "button" }).length),
    timeoutMs,
    signal,
  );

const inSettings = (selector, timeoutMs, signal) =>
  accessibility.find({ app: settings, ...selector }, { timeoutMs, signal });

async function firstFound(branches, timeoutMs) {
  const stop = new AbortController();
  let pending = branches.length;
  return new Promise((resolve, reject) => {
    if (pending === 0) {
      resolve(null);
      return;
    }
    for (const branch of branches) {
      Promise.resolve(branch(timeoutMs, stop.signal)).then(
        (found) => {
          if (found !== null) {
            stop.abort();
            resolve(found);
            return;
          }
          pending -= 1;
          if (pending === 0) {
            resolve(null);
          }
        },
        (error) => {
          if (stop.signal.aborted && error?.name === "AbortError") {
            pending -= 1;
            if (pending === 0) {
              resolve(null);
            }
            return;
          }
          stop.abort();
          reject(error);
        },
      );
    }
  });
}

async function tap(element) {
  try {
    await accessibility.tap(element, { timeoutMs: 0 });
  } catch (error) {
    if (error.name !== "TimeoutError") {
      throw error;
    }
  }
}

async function dismiss(alert, index) {
  const until = Date.now() + 10_000;
  for (;;) {
    const shown = await sheet();
    if (shown === null || shown.label !== alert.label) {
      return;
    }
    const remaining = until - Date.now();
    if (remaining <= 0) {
      throw new Error(`the alert stays: ${alert.label}`);
    }
    await tap(shown.findAll({ role: "button" })[index]);
    if ((await sheetGone(alert.label, Math.min(remaining, 1_000))) !== null) {
      return;
    }
  }
}

async function dismissPrompt(alert) {
  const buttons = alert.findAll({ role: "button" });
  if (alert.label?.includes(name) && [2, 3].includes(buttons.length)) {
    // The location alert shows an icon and lists Allow Once first; the cellular
    // data alert of China mainland devices shows none and lists Wi-Fi and
    // cellular first (iOS 27.2 24B115). The background location upgrade alert
    // has 2 buttons and lists Change to Always Allow second (iOS 27.3 24C5037i).
    const location = alert.findAll({ role: "image" }).length > 0;
    await dismiss(alert, buttons.length === 2 || location ? 1 : 0);
  }
}

async function answerPrompt() {
  if (!autoAllow) {
    return false;
  }
  const alert = await sheet();
  if (alert !== null) {
    await dismissPrompt(alert);
  }
  return alert !== null;
}

async function untrustedDeveloper() {
  const alert = await matchingSheet(
    (shown) => shown.findAll({ role: "button" }).length === 1,
    5_000,
  );
  if (alert === null) {
    throw new Error(launchError);
  }
  const developer = alert
    .findAll({ role: "text" })
    .map((text) => developerPattern.exec(text.label ?? "")?.[0])
    .find((found) => found !== undefined);
  await dismiss(alert, 0);
  if (developer === undefined) {
    throw new Error(`the alert names no developer: ${alert.label}`);
  }
  return developer;
}

function developerRows(list, developer) {
  if (list.findAll({ role: "button", identifier: "VPN" }).length === 0) {
    return [];
  }
  const elsewhere = new Set(
    list
      .findAll({
        role: "button",
        within: {
          identifier: "com.apple.settings.sidebar.collectionView",
        },
      })
      .concat(
        list.findAll({
          role: "button",
          within: { role: /^(navigationBar|searchField)$/ },
        }),
      )
      .map((row) => row.identifier),
  );
  return list
    .findAll({ role: "button" })
    .filter(
      (row) =>
        row.identifier !== undefined &&
        row.identifier !== "BackButton" &&
        row.identifier !== "VPN" &&
        !row.identifier.startsWith("com.apple.settings.") &&
        row.identifier !== row.label &&
        !elsewhere.has(row.identifier),
    )
    .sort(
      (a, b) =>
        developer.includes(b.identifier) - developer.includes(a.identifier),
    );
}

async function openDeviceManagement(developer) {
  await device.launchApplication(settings, {
    url: "prefs:root=General&path=ManagedConfigurationList",
  });
  const until = Date.now() + 10_000;
  while (Date.now() < until) {
    const remaining = Math.max(until - Date.now(), 0);
    const branches = [
      async (timeoutMs, signal) => {
        const candidate = await inSettings(
          {
            role: "button",
            within: {
              role: "application",
              has: { role: "button", identifier: "VPN" },
            },
            not: (row) =>
              row.identifier === undefined ||
              row.identifier === "BackButton" ||
              row.identifier === "VPN" ||
              row.identifier.startsWith("com.apple.settings.") ||
              row.identifier === row.label,
          },
          timeoutMs,
          signal,
        );
        return candidate === null ? null : { candidate };
      },
    ];
    if (autoAllow) {
      branches.push(async (timeoutMs, signal) => {
        const alert = await promptSheet(timeoutMs, signal);
        return alert === null ? null : { alert };
      });
    }
    const found = await firstFound(branches, remaining);
    if (found === null) {
      break;
    }
    if ("alert" in found) {
      await dismissPrompt(found.alert);
      continue;
    }
    const list = await accessibility.snapshot({ scope: "frontApp" });
    const rows = developerRows(list, developer);
    if (rows.length > 0) {
      return rows;
    }
  }
  throw new Error(`Settings lists no app from ${developer}`);
}

async function allowDeveloper(row) {
  const control = {
    role: "text",
    label: new RegExp(
      `.${escaped(row.identifier)}|${escaped(row.identifier)}.`,
    ),
  };
  if ((await inSettings(control, 1_500)) === null) {
    return false;
  }
  const until = Date.now() + 5_000;
  do {
    const shown = await inSettings(control);
    if (shown !== null) {
      await input.tap(shown.point);
    }
    const alert = await matchingSheet(
      (candidate) => candidate.findAll({ role: "button" }).length === 2,
      Math.min(Math.max(until - Date.now(), 0), 1_000),
    );
    if (alert !== null) {
      await dismiss(alert, 0);
      await inSettings(
        { role: "application", not: { has: control } },
        Math.max(until + 10_000 - Date.now(), 0),
      );
      return true;
    }
  } while (Date.now() < until);
  throw new Error("Settings did not ask to allow the developer");
}

async function trust(developer) {
  console.debug(filled(messages.trusting, developer));
  const page = { label: new RegExp(escaped(developer)) };
  for (const row of await openDeviceManagement(developer)) {
    const until = Date.now() + 3_000;
    let opened = null;
    do {
      await tap(row);
      opened = await inSettings(
        page,
        Math.min(Math.max(until - Date.now(), 0), 1_000),
      );
    } while (opened === null && Date.now() < until);
    if (opened !== null && (await allowDeveloper(row))) {
      return;
    }
    const button = await inSettings({ identifier: "BackButton" }, 3_000);
    if (button === null) {
      throw new Error("Settings shows no back button");
    }
    await tap(button);
  }
  throw new Error(`Settings lists no app from ${developer}`);
}

async function waitForUnlock() {
  for await (const locked of device.locked.values()) {
    if (!locked) {
      return;
    }
    console.debug(messages.unlockToContinue);
  }
}

async function allowLocation() {
  await device.launchApplication(settings, {
    url: `app-prefs:${id}`,
    restart: true,
  });
  const until = Date.now() + 10_000;
  let row = null;
  while (row === null && Date.now() < until) {
    const remaining = Math.max(until - Date.now(), 0);
    const branches = [
      async (timeoutMs, signal) => {
        const button = await inSettings(
          { role: "button", identifier: id },
          timeoutMs,
          signal,
        );
        return button === null ? null : { row: button };
      },
    ];
    if (autoAllow) {
      branches.push(async (timeoutMs, signal) => {
        const alert = await promptSheet(timeoutMs, signal);
        return alert === null ? null : { alert };
      });
    }
    const found = await firstFound(branches, remaining);
    if (found === null) {
      break;
    }
    if ("alert" in found) {
      await dismissPrompt(found.alert);
    } else {
      row = found.row;
    }
  }
  if (row === null) {
    await device.launchApplication(id);
    return;
  }
  let option = null;
  do {
    await tap(row);
    option = await inSettings(
      { identifier: "LOCATION_SERVICES_AUTH_ALWAYS" },
      Math.min(Math.max(until - Date.now(), 0), 1_000),
    );
  } while (option === null && Date.now() < until);
  if (option !== null && !option.selected && !option.disabled) {
    let selected = null;
    do {
      const target =
        option ??
        (await inSettings(
          { identifier: "LOCATION_SERVICES_AUTH_ALWAYS" },
          Math.min(Math.max(until - Date.now(), 0), 1_000),
        ));
      option = null;
      if (target !== null) {
        if (target.selected || target.disabled) {
          break;
        }
        await input.tap(target.point);
        selected = await inSettings(
          { identifier: "LOCATION_SERVICES_AUTH_ALWAYS", selected: true },
          Math.min(Math.max(until - Date.now(), 0), 1_000),
        );
      }
    } while (selected === null && Date.now() < until);
  }
  await device.launchApplication(id);
}

async function finishSetup() {
  const until = Date.now() + 60_000;
  let finished = null;
  while (finished === null && Date.now() < until) {
    try {
      const remaining = Math.max(until - Date.now(), 0);
      const branches = [
        async (timeoutMs, signal) => {
          const marker = await accessibility.find(
            { app: id, identifier: "setup-finished" },
            { timeoutMs, signal },
          );
          return marker === null ? null : { finished: marker };
        },
      ];
      if (autoAllow) {
        branches.push(async (timeoutMs, signal) => {
          const alert = await promptSheet(timeoutMs, signal);
          return alert === null ? null : { alert };
        });
      }
      const found = await firstFound(branches, remaining);
      if (found === null) {
        break;
      }
      if ("alert" in found) {
        await dismissPrompt(found.alert);
      } else {
        finished = found.finished;
      }
    } catch (error) {
      if (
        error.name !== "TimeoutError" &&
        !/stopped answering/.test(error.message)
      ) {
        throw error;
      }
    }
  }
  if (finished === null) {
    throw new Error(`${name} did not finish setting up`);
  }
  if (finished.value !== "always") {
    await allowLocation();
  }
  console.log(filled(messages.running, name));
}

async function main() {
  switch (mode) {
    case "unlock":
      if (unlock === "passcode") {
        console.debug(messages.unlocking);
        await device.unlock();
      } else if (unlock === "wait") {
        await waitForUnlock();
      }
      await answerPrompt();
      return;
    case "trust": {
      const developer = await untrustedDeveloper();
      await trust(developer);
      console.log(filled(messages.trusted, developer));
      return;
    }
    case "prompts":
      await finishSetup();
      return;
  }
}

await main();
