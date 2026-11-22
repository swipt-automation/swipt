import dgram from "node:dgram";
import os from "node:os";

import type { StringAnswer } from "dns-packet";
import multicastDNS from "multicast-dns";

const requery = 300;
const responseFlag = 0x8000;
const unicastQuestion = 0x8000;

function requestUnicast(packet: Buffer) {
  if (packet.length < 12 || (packet.readUInt16BE(2) & responseFlag) !== 0) {
    return;
  }
  const questions = packet.readUInt16BE(4);
  let offset = 12;
  for (let index = 0; index < questions; index++) {
    while (offset < packet.length) {
      const length = packet[offset];
      if (length === 0) {
        offset += 1;
        break;
      }
      if ((length & 0xc0) === 0xc0) {
        offset += 2;
        break;
      }
      offset += 1 + length;
    }
    if (offset + 4 > packet.length) {
      return;
    }
    packet.writeUInt16BE(
      packet.readUInt16BE(offset + 2) | unicastQuestion,
      offset + 2,
    );
    offset += 4;
  }
}

function unicastSocket(address: string) {
  const socket = dgram.createSocket("udp4");
  const send = socket.send.bind(socket);
  let state: "unbound" | "binding" | "bound" | "closed" = "unbound";
  const queued: (() => void)[] = [];
  socket.once("close", () => {
    state = "closed";
    queued.length = 0;
  });
  socket.send = ((
    msg: string | Uint8Array | readonly Uint8Array[],
    ...rest: unknown[]
  ) => {
    if (Buffer.isBuffer(msg)) {
      requestUnicast(msg);
    }
    const invoke = () => {
      if (state === "bound") {
        (send as (...args: unknown[]) => void)(msg, ...rest);
      }
    };
    if (state === "bound") {
      invoke();
      return;
    }
    if (state === "closed") {
      return;
    }
    queued.push(invoke);
    if (state === "unbound") {
      state = "binding";
      socket.bind(0, address, () => {
        if (state !== "binding") {
          return;
        }
        state = "bound";
        for (const pending of queued.splice(0)) {
          pending();
        }
      });
    }
  }) as typeof socket.send;
  return socket;
}

export const multicastOptions = () =>
  Object.values(os.networkInterfaces())
    .map((addresses) =>
      addresses?.find((it) => it.family === "IPv4" && !it.internal),
    )
    .filter((found) => found !== undefined)
    .map(({ address }) => ({
      socket: unicastSocket(address),
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
