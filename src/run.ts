import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";

export interface RunOptions {
  command?: string[];
  cwd?: string;
  env?: Record<string, string>;
  stdio?: "inherit" | "pipe";
}

const DEFAULT_REMOTE_RUN_COMMAND = ["go", "run", "github.com/batuhan/easymatrix/cmd/server@latest"];

function findLocalRepoRoot(start: string): string | null {
  let current = start;
  while (true) {
    if (existsSync(join(current, "cmd", "server"))) {
      return current;
    }
    const parent = dirname(current);
    if (parent === current) {
      return null;
    }
    current = parent;
  }
}

function resolveDefaultCommand(cwd: string): string[] {
  const localRepoRoot = findLocalRepoRoot(cwd);
  if (localRepoRoot) {
    return ["go", "run", "./cmd/server"];
  }
  return DEFAULT_REMOTE_RUN_COMMAND;
}

export function run(options: RunOptions = {}): ChildProcess {
  const cwd = options.cwd ?? process.cwd();
  const localRepoRoot = findLocalRepoRoot(cwd);
  const command =
    options.command && options.command.length > 0 ? options.command : resolveDefaultCommand(cwd);
  const [bin, ...args] = command;
  if (!bin) {
    throw new Error("Invalid run command: command cannot be empty.");
  }

  return spawn(bin, args, {
    cwd: localRepoRoot ?? cwd,
    env: {
      ...process.env,
      ...options.env,
    },
    stdio: options.stdio ?? "inherit",
  });
}
