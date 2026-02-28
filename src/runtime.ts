import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export interface EmbeddedRuntimeOptions {
  command?: string[];
  cwd?: string;
  env?: Record<string, string>;
  listenAddr?: string;
  accessToken?: string;
  stateDir?: string;
  startupTimeoutMs?: number;
  pollIntervalMs?: number;
  inheritStdio?: boolean;
}

export interface EmbeddedRuntimeStatus {
  baseURL: string;
  listenAddr: string;
  running: boolean;
}

const DEFAULT_LISTEN_ADDR = "127.0.0.1:23373";
const DEFAULT_STARTUP_TIMEOUT_MS = 20_000;
const DEFAULT_POLL_INTERVAL_MS = 250;

const RUNTIME_FILE_DIR = dirname(fileURLToPath(import.meta.url));
const DEFAULT_REPO_ROOT = resolve(RUNTIME_FILE_DIR, "..");
const DEFAULT_BIN_PATH = resolve(DEFAULT_REPO_ROOT, "bin", "easymatrix");

function delay(ms: number): Promise<void> {
  return new Promise((resolveDelay) => {
    setTimeout(resolveDelay, ms);
  });
}

function listenAddrToBaseURL(listenAddr: string): string {
  if (listenAddr.startsWith("http://") || listenAddr.startsWith("https://")) {
    return listenAddr;
  }
  return `http://${listenAddr}`;
}

function resolveCommandAndCWD(options: EmbeddedRuntimeOptions): { command: string[]; cwd?: string } {
  if (options.command && options.command.length > 0) {
    return {
      command: options.command,
      cwd: options.cwd,
    };
  }

  if (existsSync(DEFAULT_BIN_PATH)) {
    return {
      command: [DEFAULT_BIN_PATH],
      cwd: options.cwd,
    };
  }

  return {
    command: ["go", "run", "./cmd/server"],
    cwd: options.cwd ?? DEFAULT_REPO_ROOT,
  };
}

export class EmbeddedRuntime {
  readonly options: Readonly<EmbeddedRuntimeOptions>;
  readonly listenAddr: string;
  readonly baseURL: string;

  private process?: Bun.Subprocess;

  constructor(options: EmbeddedRuntimeOptions = {}) {
    this.options = options;
    this.listenAddr = options.listenAddr ?? DEFAULT_LISTEN_ADDR;
    this.baseURL = listenAddrToBaseURL(this.listenAddr);
  }

  status(): EmbeddedRuntimeStatus {
    return {
      baseURL: this.baseURL,
      listenAddr: this.listenAddr,
      running: Boolean(this.process && this.process.exitCode == null),
    };
  }

  async start(): Promise<void> {
    if (this.process && this.process.exitCode == null) {
      return;
    }

    const commandAndCWD = resolveCommandAndCWD(this.options);

    const env: Record<string, string> = {
      ...Bun.env,
      ...this.options.env,
      BEEPER_API_LISTEN: this.listenAddr,
    };
    if (this.options.accessToken) {
      env.BEEPER_ACCESS_TOKEN = this.options.accessToken;
    }
    if (this.options.stateDir) {
      env.BEEPER_STATE_DIR = this.options.stateDir;
    }

    this.process = Bun.spawn(commandAndCWD.command, {
      cwd: commandAndCWD.cwd,
      env,
      stdout: this.options.inheritStdio ? "inherit" : "pipe",
      stderr: this.options.inheritStdio ? "inherit" : "pipe",
    });

    try {
      await this.waitForReady();
    } catch (error) {
      this.stop().catch(() => {
        // ignore cleanup failures on startup errors
      });
      throw error;
    }
  }

  async stop(): Promise<void> {
    if (!this.process) {
      return;
    }

    const proc = this.process;
    this.process = undefined;

    if (proc.exitCode == null) {
      proc.kill();
    }

    await proc.exited;
  }

  private async waitForReady(): Promise<void> {
    const startupTimeoutMs = this.options.startupTimeoutMs ?? DEFAULT_STARTUP_TIMEOUT_MS;
    const pollIntervalMs = this.options.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
    const startedAt = Date.now();
    const url = `${this.baseURL}/v1/info`;

    while (Date.now() - startedAt < startupTimeoutMs) {
      if (!this.process || this.process.exitCode != null) {
        const logs = await this.readProcessLogs();
        throw new Error(`Embedded runtime exited before becoming ready. ${logs}`.trim());
      }

      try {
        const response = await fetch(url, { method: "GET" });
        if (response.ok) {
          return;
        }
      } catch {
        // process may still be starting
      }

      await delay(pollIntervalMs);
    }

    const logs = await this.readProcessLogs();
    throw new Error(`Timed out waiting for embedded runtime readiness at ${url}. ${logs}`.trim());
  }

  private async readProcessLogs(): Promise<string> {
    if (!this.process || this.options.inheritStdio) {
      return "";
    }

    const chunks: string[] = [];

    if (this.process.stderr && typeof this.process.stderr !== "number") {
      const stderrText = await new Response(this.process.stderr).text();
      if (stderrText.trim()) {
        chunks.push(`stderr: ${stderrText.trim()}`);
      }
    }

    if (this.process.stdout && typeof this.process.stdout !== "number") {
      const stdoutText = await new Response(this.process.stdout).text();
      if (stdoutText.trim()) {
        chunks.push(`stdout: ${stdoutText.trim()}`);
      }
    }

    return chunks.join(" | ");
  }
}
