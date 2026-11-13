import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";

const localHeader = 0x04034b50;
const centralHeader = 0x02014b50;
const endRecord = 0x06054b50;
const utf8Flag = 0x0800;
const madeByUnix = 3 << 8;
const version = 20;

function dosTime(date: Date) {
  return {
    time:
      (date.getHours() << 11) |
      (date.getMinutes() << 5) |
      Math.floor(date.getSeconds() / 2),
    date:
      ((Math.max(date.getFullYear(), 1980) - 1980) << 9) |
      ((date.getMonth() + 1) << 5) |
      date.getDate(),
  };
}

function files(folder: string, prefix = ""): string[] {
  return fs
    .readdirSync(path.join(folder, prefix), { withFileTypes: true })
    .sort((a, b) => (a.name < b.name ? -1 : 1))
    .flatMap((entry) => {
      const name = prefix ? `${prefix}/${entry.name}` : entry.name;
      return entry.isDirectory() ? files(folder, name) : [name];
    });
}

export function writeZip(
  archive: string,
  folder: string,
  storedSuffixes: readonly string[],
) {
  const parts: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;
  for (const name of files(folder)) {
    const file = path.join(folder, name);
    const data = fs.readFileSync(file);
    const stat = fs.statSync(file);
    const deflated = storedSuffixes.some((suffix) =>
      name.toLowerCase().endsWith(suffix),
    )
      ? undefined
      : zlib.deflateRawSync(data);
    const compressed =
      deflated !== undefined && deflated.length < data.length ? deflated : data;
    const method = compressed === data ? 0 : 8;
    const { time, date } = dosTime(stat.mtime);
    const encoded = Buffer.from(name, "utf8");
    const crc = zlib.crc32(data);
    if (offset > 0xffffffff || data.length > 0xffffffff) {
      throw new Error(`${archive} would need ZIP64`);
    }
    const local = Buffer.alloc(30);
    local.writeUInt32LE(localHeader, 0);
    local.writeUInt16LE(version, 4);
    local.writeUInt16LE(utf8Flag, 6);
    local.writeUInt16LE(method, 8);
    local.writeUInt16LE(time, 10);
    local.writeUInt16LE(date, 12);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(compressed.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(encoded.length, 26);
    const header = Buffer.alloc(46);
    header.writeUInt32LE(centralHeader, 0);
    header.writeUInt16LE(madeByUnix | version, 4);
    header.writeUInt16LE(version, 6);
    header.writeUInt16LE(utf8Flag, 8);
    header.writeUInt16LE(method, 10);
    header.writeUInt16LE(time, 12);
    header.writeUInt16LE(date, 14);
    header.writeUInt32LE(crc, 16);
    header.writeUInt32LE(compressed.length, 20);
    header.writeUInt32LE(data.length, 24);
    header.writeUInt16LE(encoded.length, 28);
    header.writeUInt32LE((((stat.mode & 0o777) | 0o100000) << 16) >>> 0, 38);
    header.writeUInt32LE(offset, 42);
    parts.push(local, encoded, compressed);
    central.push(header, encoded);
    offset += local.length + encoded.length + compressed.length;
  }
  if (central.length / 2 > 0xffff) {
    throw new Error(`${archive} would need ZIP64`);
  }
  const directory = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(endRecord, 0);
  end.writeUInt16LE(central.length / 2, 8);
  end.writeUInt16LE(central.length / 2, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  fs.writeFileSync(archive, Buffer.concat([...parts, directory, end]));
}

export interface ZipEntry {
  readonly name: string;
  read(): Buffer;
}

export function readZip(archive: string): ZipEntry[] {
  const data = fs.readFileSync(archive);
  let end = data.length - 22;
  while (end >= 0 && data.readUInt32LE(end) !== endRecord) {
    end--;
  }
  if (end < 0) {
    throw new Error(`${archive} is not a ZIP archive`);
  }
  const count = data.readUInt16LE(end + 10);
  let position = data.readUInt32LE(end + 16);
  const entries: ZipEntry[] = [];
  for (let index = 0; index < count; index++) {
    if (data.readUInt32LE(position) !== centralHeader) {
      throw new Error(`${archive} has a damaged central directory`);
    }
    const method = data.readUInt16LE(position + 10);
    const size = data.readUInt32LE(position + 20);
    const nameLength = data.readUInt16LE(position + 28);
    const skipped =
      data.readUInt16LE(position + 30) + data.readUInt16LE(position + 32);
    const local = data.readUInt32LE(position + 42);
    const name = data.toString(
      "utf8",
      position + 46,
      position + 46 + nameLength,
    );
    entries.push({
      name,
      read() {
        const start =
          local +
          30 +
          data.readUInt16LE(local + 26) +
          data.readUInt16LE(local + 28);
        const content = data.subarray(start, start + size);
        return method === 8 ? zlib.inflateRawSync(content) : content;
      },
    });
    position += 46 + nameLength + skipped;
  }
  return entries;
}
