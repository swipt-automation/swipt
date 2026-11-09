export interface Option {
  type: "string" | "boolean";
  short?: string;
  value?: string;
  help: string;
}

export type Options = Record<string, Option>;

export interface Command {
  name: string;
  use?: string;
  short: string;
  description?: string[];
  args?: [least: number, most: number];
  options?: Options;
  commands?: Command[];
  passthrough?: (word: string) => boolean;
  parse?: (values: Record<string, any>) => Record<string, any>;
  run?: (context: any) => unknown;
}

export const defineCommand = (command: Command): Command => command;
