#!/usr/bin/env node

import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";

function findLocalRepoRoot(start) {
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

const cwd = process.cwd();
const localRepoRoot = findLocalRepoRoot(cwd);
const defaultCommand = localRepoRoot
  ? ["go", "run", "./cmd/server"]
  : ["go", "run", "github.com/batuhan/easymatrix/cmd/server@latest"];
const userArgs = process.argv.slice(2);
const command = [...defaultCommand, ...userArgs];

const [bin, ...args] = command;
const child = spawn(bin, args, {
  stdio: "inherit",
  env: process.env,
  cwd: localRepoRoot ?? cwd,
});

child.on("error", (error) => {
  console.error(`[easymatrix] failed to start: ${error.message}`);
  console.error("[easymatrix] ensure Go is installed and available in PATH.");
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 1);
});
