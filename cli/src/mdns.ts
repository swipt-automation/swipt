import dgram from "node:dgram";
import os from "node:os";

import type { StringAnswer } from "dns-packet";
import multicastDNS from "multicast-dns";

const requery = 300;

export const multicastOptions = () =>
  Object.values(os.networkInterfaces())
    .map((addresses) =>
      addresses?.find((it) => it.family === "IPv4" && !it.internal),
    )
    .filter((found) => found !== undefined)
    .map(({ address }) => ({
      socket: dgram.createSocket("udp4"),
      bind: false as const,
      interface: address,
    }));

export async function resolveHost(host: string, signal: AbortSignal) {
  const sockets = multicastOptions().map((options) => multicastDNS(options));
  const query = () => {
    for (const mdns of sockets) {
      mdns.query([
        { name: host, type: "A" },
        { name: host, type: "AAAA" },
      ]);
    }
  };
  const timer = setInterval(query, requery);
  try {
    return await new Promise<string | undefined>((resolve, reject) => {
      if (signal.aborted) {
        resolve(undefined);
      }
      signal.addEventListener("abort", () => resolve(undefined), {
        once: true,
      });
      for (const mdns of sockets) {
        mdns.on("error", reject);
        mdns.on("response", (response) => {
          const answers = response.answers.filter(
            (it): it is StringAnswer =>
              it.name.toLowerCase() === host.toLowerCase() &&
              (it.type === "A" ||
                (it.type === "AAAA" && !/^fe[89ab]/i.test(it.data))),
          );
          const answer = answers.find((it) => it.type === "A") ?? answers[0];
          if (answer) {
            resolve(answer.data);
          }
        });
      }
      query();
    });
  } finally {
    clearInterval(timer);
    for (const mdns of sockets) {
      mdns.destroy();
    }
  }
}
