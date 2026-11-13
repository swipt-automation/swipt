export interface Option {
  type: "string" | "boolean";
  short?: string;
  value?: string;
  help: string;
}

export type Options = Record<string, Option>;

export interface Context<Values = {}> {
  args: string[];
  options: Values;
  target: { url: string };
  signal: AbortSignal;
  printError: (error: unknown, operation?: string) => number;
}

interface Named {
  name: string;
  use?: string;
  short: string;
  description?: string[];
  options?: Options;
}

export interface CommandGroup extends Named {
  commands: Command[];
}

export interface LeafCommand extends Named {
  args: [least: number, most: number];
  passthrough?: (word: string) => boolean;
  run: (context: Context<any>) => unknown;
}

export type Command = CommandGroup | LeafCommand;

export const defineCommand = <C extends Command>(command: C): C => command;
