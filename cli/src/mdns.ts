import crypto from "node:crypto";
import dgram from "node:dgram";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";

import * as dnsPacket from "dns-packet";
import type {
  Answer,
  RecordType,
  SrvAnswer,
  StringAnswer,
  TxtAnswer,
} from "dns-packet";
import multicastDNS from "multicast-dns";

const requery = 300;
const responseFlag = 0x8000;
const unicastQuestion = 0x8000;
const hostResolvConf = "/run/host/resolv.conf";

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

function hostNameServers() {
  let text;
  try {
    text = fs.readFileSync(hostResolvConf, "utf8");
  } catch {
    return [];
  }
  const servers: string[] = [];
  for (const line of text.split("\n")) {
    const match = /^\s*nameserver\s+([^\s#;]+)/.exec(line);
    if (match && net.isIP(match[1]) !== 0) {
      servers.push(match[1]);
    }
  }
  return servers;
}

async function queryHostDNS(
  server: string,
  name: string,
  type: RecordType,
  signal: AbortSignal,
): Promise<Answer[]> {
  if (signal.aborted) {
    return [];
  }
  const id = crypto.randomInt(1, 0x10000);
  const message = dnsPacket.encode({
    id,
    type: "query",
    flags: dnsPacket.RECURSION_DESIRED,
    questions: [{ type, name }],
    additionals: [
      {
        type: "OPT",
        name: ".",
        udpPayloadSize: 4096,
        extendedRcode: 0,
        ednsVersion: 0,
        flags: 0,
        flag_do: false,
        options: [],
      },
    ],
  });
  const socket = dgram.createSocket(net.isIP(server) === 6 ? "udp6" : "udp4");
  const send = () => {
    socket.send(message, 53, server);
  };
  const timer = setInterval(send, requery);
  try {
    return await new Promise<Answer[]>((resolve) => {
      const finish = (answers: Answer[]) => {
        signal.removeEventListener("abort", onAbort);
        resolve(answers);
      };
      const onAbort = () => finish([]);
      if (signal.aborted) {
        finish([]);
        return;
      }
      signal.addEventListener("abort", onAbort, { once: true });
      socket.on("error", () => finish([]));
      socket.on("message", (data) => {
        try {
          const decoded = dnsPacket.decode(data);
          if (decoded.type === "response" && decoded.id === id) {
            finish(decoded.answers ?? []);
          }
        } catch {}
      });
      send();
    });
  } finally {
    clearInterval(timer);
    socket.close();
  }
}

function decodeTxt(answers: Answer[]) {
  const txt: Record<string, string> = {};
  for (const answer of answers) {
    if (answer.type !== "TXT") {
      continue;
    }
    const items = Array.isArray((answer as TxtAnswer).data)
      ? ((answer as TxtAnswer).data as Array<string | Buffer>)
      : [(answer as TxtAnswer).data as string | Buffer];
    for (const item of items) {
      const entry = typeof item === "string" ? item : item.toString("utf8");
      const equals = entry.indexOf("=");
      if (equals > 0) {
        txt[entry.slice(0, equals).toLowerCase()] = entry.slice(equals + 1);
      } else if (equals < 0 && entry !== "") {
        txt[entry.toLowerCase()] = "";
      }
    }
  }
  return txt;
}

export interface UnicastService {
  name: string;
  host: string;
  port: number;
  txt: Record<string, string>;
}

export async function browseHostDNS(
  type: string,
  signal: AbortSignal,
): Promise<UnicastService[] | undefined> {
  const [server] = hostNameServers();
  if (server === undefined) {
    return undefined;
  }
  const suffix = `._${type}._tcp.local`;
  const ptrs = (await queryHostDNS(server, `${suffix.slice(1)}`, "PTR", signal))
    .filter((it): it is StringAnswer => it.type === "PTR")
    .map((it) => it.data.replace(/\.$/, ""));
  const suffixPattern = new RegExp(
    `${suffix.replaceAll(".", "\\.")}$`,
    "i",
  );
  const resolved = await Promise.all(
    [...new Set(ptrs)].map(async (fqdn): Promise<UnicastService | undefined> => {
      const [srvs, txts] = await Promise.all([
        queryHostDNS(server, fqdn, "SRV", signal),
        queryHostDNS(server, fqdn, "TXT", signal),
      ]);
      const srv = srvs.find((it): it is SrvAnswer => it.type === "SRV");
      if (!srv) {
        return undefined;
      }
      return {
        name: fqdn.replace(suffixPattern, ""),
        host: srv.data.target,
        port: srv.data.port,
        txt: decodeTxt(txts),
      };
    }),
  );
  return resolved.filter((it): it is UnicastService => it !== undefined);
}

export async function resolveHost(host: string, signal: AbortSignal) {
  const [server] = hostNameServers();
  if (server !== undefined) {
    const [v4, v6] = await Promise.all([
      queryHostDNS(server, host, "A", signal),
      queryHostDNS(server, host, "AAAA", signal),
    ]);
    const ipv4 = v4.find((it): it is StringAnswer => it.type === "A");
    if (ipv4) {
      return ipv4.data;
    }
    const ipv6 = v6.find(
      (it): it is StringAnswer =>
        it.type === "AAAA" && !/^fe[89ab]/i.test(it.data),
    );
    return ipv6?.data;
  }

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
