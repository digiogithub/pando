#!/usr/bin/env bun
/**
 * acp-debug-client.ts
 *
 * Interactive ACP debug client for Pando.
 * Launches `pando acp` as a subprocess (stdio transport),
 * handles JSON-RPC 2.0 protocol, and shows all server traces.
 *
 * Usage:
 *   bun scripts/acp-debug-client.ts [--cwd <dir>] [--model <model>] [--auto-permission]
 *
 * Example:
 *   bun scripts/acp-debug-client.ts --model copilot.gpt-4.1
 */

import { spawn } from "bun";
import * as readline from "readline";

// ─── Config ──────────────────────────────────────────────────────────────────

const DEFAULT_MODEL = "copilot.gpt-4.1";
const CLIENT_NAME = "acp-debug-client";
const CLIENT_VERSION = "1.0.0";

// ─── ANSI colors ─────────────────────────────────────────────────────────────

const C = {
  reset: "\x1b[0m",
  bold: "\x1b[1m",
  dim: "\x1b[2m",
  red: "\x1b[31m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  blue: "\x1b[34m",
  magenta: "\x1b[35m",
  cyan: "\x1b[36m",
  white: "\x1b[37m",
  gray: "\x1b[90m",
  bgBlue: "\x1b[44m",
  bgGray: "\x1b[100m",
};

// ─── Logger ───────────────────────────────────────────────────────────────────

function ts(): string {
  return new Date().toISOString().substring(11, 23);
}

function logTrace(dir: "→" | "←" | "•", color: string, label: string, data: unknown) {
  const prefix = `${C.dim}${ts()}${C.reset} ${color}${dir} ${label}${C.reset}`;
  if (typeof data === "string") {
    process.stderr.write(`${prefix} ${C.dim}${data}${C.reset}\n`);
  } else {
    const json = JSON.stringify(data, null, 2)
      .split("\n")
      .map((l, i) => (i === 0 ? l : `        ${l}`))
      .join("\n");
    process.stderr.write(`${prefix}\n        ${C.dim}${json}${C.reset}\n`);
  }
}

function logInfo(msg: string) {
  process.stderr.write(`${C.dim}${ts()}${C.reset} ${C.cyan}ℹ ${msg}${C.reset}\n`);
}

function logError(msg: string) {
  process.stderr.write(`${C.dim}${ts()}${C.reset} ${C.red}✖ ${msg}${C.reset}\n`);
}

function logAssistant(text: string) {
  process.stdout.write(`\n${C.bold}${C.green}assistant${C.reset} ${text}`);
}

function logTool(toolName: string, title: string) {
  process.stdout.write(`\n${C.bold}${C.yellow}tool:${toolName}${C.reset} ${C.dim}${title}${C.reset}\n`);
}

// ─── JSON-RPC types ───────────────────────────────────────────────────────────

interface JsonRpcRequest {
  jsonrpc: "2.0";
  id: number;
  method: string;
  params?: unknown;
}

interface JsonRpcNotification {
  jsonrpc: "2.0";
  method: string;
  params?: unknown;
}

interface JsonRpcResponse {
  jsonrpc: "2.0";
  id?: number;
  result?: unknown;
  error?: { code: number; message: string; data?: unknown };
  method?: string;
  params?: unknown;
}

// ─── ACP Client ───────────────────────────────────────────────────────────────

class ACPClient {
  private proc: ReturnType<typeof spawn>;
  private nextId = 1;
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();
  private sessionId: string | null = null;
  private lineBuffer = "";
  private streamText = "";

  constructor(proc: ReturnType<typeof spawn>) {
    this.proc = proc;
  }

  /** Write a raw line to the server's stdin (Bun FileSink). */
  private writeStdin(line: string) {
    // In Bun, proc.stdin is a FileSink with a synchronous write() + flush()
    const sink = this.proc.stdin as import("bun").FileSink;
    sink.write(line);
    sink.flush();
  }

  /** Send a JSON-RPC request and await its response. */
  async request(method: string, params?: unknown): Promise<unknown> {
    const id = this.nextId++;
    const msg: JsonRpcRequest = { jsonrpc: "2.0", id, method, params };
    const line = JSON.stringify(msg) + "\n";

    logTrace("→", C.blue, `SEND [${id}] ${method}`, params ?? {});
    this.writeStdin(line);

    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });
  }

  /** Send a JSON-RPC notification (no response expected). */
  async notify(method: string, params?: unknown): Promise<void> {
    const msg: JsonRpcNotification = { jsonrpc: "2.0", method, params };
    const line = JSON.stringify(msg) + "\n";
    logTrace("→", C.magenta, `NOTIFY ${method}`, params ?? {});
    this.writeStdin(line);
  }

  /** Start reading stdout from the server process. */
  startReading() {
    (async () => {
      const reader = this.proc.stdout.getReader();
      const decoder = new TextDecoder();
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          this.lineBuffer += decoder.decode(value, { stream: true });
          const lines = this.lineBuffer.split("\n");
          this.lineBuffer = lines.pop() ?? "";
          for (const line of lines) {
            const trimmed = line.trim();
            if (trimmed) this.handleLine(trimmed);
          }
        }
      } catch (e) {
        logError(`stdout reader error: ${e}`);
      }
    })();
  }

  /** Read and stream server stderr (logs/traces). */
  startReadingStderr() {
    (async () => {
      const reader = this.proc.stderr.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          const lines = buf.split("\n");
          buf = lines.pop() ?? "";
          for (const line of lines) {
            if (line.trim()) {
              process.stderr.write(`${C.dim}${ts()}${C.reset} ${C.gray}[server] ${line}${C.reset}\n`);
            }
          }
        }
      } catch (e) {
        logError(`stderr reader error: ${e}`);
      }
    })();
  }

  private handleLine(line: string) {
    let msg: JsonRpcResponse;
    try {
      msg = JSON.parse(line) as JsonRpcResponse;
    } catch {
      logTrace("←", C.red, "PARSE ERROR", line);
      return;
    }

    // Response to a pending request
    if (msg.id !== undefined && msg.id !== null) {
      const pendingId = msg.id as number;
      logTrace("←", C.green, `RECV [${pendingId}]`, msg.result ?? msg.error);
      const pending = this.pending.get(pendingId);
      if (pending) {
        this.pending.delete(pendingId);
        if (msg.error) {
          pending.reject(new Error(`[${msg.error.code}] ${msg.error.message}`));
        } else {
          pending.resolve(msg.result);
        }
      } else {
        logTrace("←", C.yellow, `UNKNOWN ID [${pendingId}]`, msg);
      }
      return;
    }

    // Notification / server push
    if (msg.method) {
      logTrace("←", C.magenta, `NOTIF ${msg.method}`, msg.params ?? {});
      this.handleNotification(msg.method, msg.params);
    }
  }

  private handleNotification(method: string, params: unknown) {
    // The SDK sends "session/update" notifications to the client
    if (method !== "session/update") return;

    const p = params as {
      sessionId?: string;
      update?: {
        type?: string;
        text?: string;
        role?: string;
        toolCallId?: string;
        toolName?: string;
        title?: string;
        content?: string;
        availableCommands?: Array<{ name: string; description: string }>;
      };
    };

    const upd = p?.update;
    if (!upd) return;

    switch (upd.type) {
      case "text":
        if (upd.role === "assistant" || !upd.role) {
          // Buffer streaming text and display inline
          if (this.streamText === "") {
            process.stdout.write(`\n${C.bold}${C.green}assistant${C.reset} `);
          }
          this.streamText += upd.text ?? "";
          process.stdout.write(upd.text ?? "");
        }
        break;

      case "tool_call":
        if (this.streamText) {
          process.stdout.write("\n");
          this.streamText = "";
        }
        logTool(upd.toolName ?? "?", upd.title ?? "");
        break;

      case "tool_result":
        // Already shown via tool_call; nothing extra needed
        break;

      case "available_commands_update":
        if (upd.availableCommands?.length) {
          logInfo(
            `Available commands: ${upd.availableCommands.map((c) => c.name).join(", ")}`
          );
        }
        break;

      default:
        // Already logged by handleLine trace
        break;
    }
  }

  getSessionId(): string | null {
    return this.sessionId;
  }

  setSessionId(id: string) {
    this.sessionId = id;
  }

  resetStreamText() {
    this.streamText = "";
  }
}

// ─── CLI argument parsing ─────────────────────────────────────────────────────

function parseArgs(): { cwd: string; model: string; autoPermission: boolean; pandoCmd: string } {
  const args = process.argv.slice(2);
  let cwd = process.cwd();
  let model = DEFAULT_MODEL;
  let autoPermission = false;
  let pandoCmd = "pando";

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--cwd" && args[i + 1]) cwd = args[++i];
    else if (args[i] === "--model" && args[i + 1]) model = args[++i];
    else if (args[i] === "--auto-permission") autoPermission = true;
    else if (args[i] === "--pando" && args[i + 1]) pandoCmd = args[++i];
    else if (args[i] === "--help" || args[i] === "-h") {
      console.log(`
${C.bold}acp-debug-client${C.reset} — Interactive ACP debug client for Pando

${C.bold}Usage:${C.reset}
  bun scripts/acp-debug-client.ts [options]

${C.bold}Options:${C.reset}
  --cwd <dir>          Working directory for the ACP session (default: current dir)
  --model <model>      Model to use (default: ${DEFAULT_MODEL})
  --auto-permission    Auto-approve all tool permission requests
  --pando <cmd>        Path/command to pando binary (default: pando)
  --help, -h           Show this help

${C.bold}Commands during chat:${C.reset}
  /quit or /exit       Exit the client
  /model <id>          Switch model for current session
  /mode <agent|ask>    Switch session mode
  /session             Show current session ID
  /sessions            List available sessions
  /personas            List available personas
  /persona <name>      Set persona for current session
  /cancel              Cancel current running prompt
  /help                Show this help in chat
`);
      process.exit(0);
    }
  }
  return { cwd, model, autoPermission, pandoCmd };
}

// ─── Main ─────────────────────────────────────────────────────────────────────

async function main() {
  const { cwd, model, autoPermission, pandoCmd } = parseArgs();

  console.log(`
${C.bold}${C.cyan}╔═══════════════════════════════════════╗
║     Pando ACP Debug Client v1.0       ║
╚═══════════════════════════════════════╝${C.reset}
  Model   : ${C.yellow}${model}${C.reset}
  Cwd     : ${C.dim}${cwd}${C.reset}
  Traces  : ${C.dim}stderr${C.reset}

${C.dim}Type /help for available commands. Ctrl+C or /quit to exit.${C.reset}
`);

  // ── Spawn pando acp ──
  const pandoArgs = ["acp"];
  if (autoPermission) pandoArgs.push("--auto-permission");

  logInfo(`Spawning: ${pandoCmd} ${pandoArgs.join(" ")}`);

  const proc = spawn({
    cmd: [pandoCmd, ...pandoArgs],
    stdin: "pipe",
    stdout: "pipe",
    stderr: "pipe",
    env: { ...process.env },
  });

  const client = new ACPClient(proc);
  client.startReading();
  client.startReadingStderr();

  // ── Handle process exit ──
  proc.exited.then((code) => {
    logInfo(`Pando process exited with code ${code}`);
    process.exit(code ?? 0);
  });

  // ── Initialize ──
  logInfo("Sending initialize…");
  const initResult = (await client.request("initialize", {
    protocolVersion: 1,
    clientInfo: { name: CLIENT_NAME, version: CLIENT_VERSION },
    clientCapabilities: {
      fs: { readTextFile: true, writeTextFile: false },
      terminal: false,
    },
  })) as { agentInfo?: { name: string; version: string }; agentCapabilities?: unknown };

  logInfo(
    `Connected to: ${initResult?.agentInfo?.name ?? "pando"} v${initResult?.agentInfo?.version ?? "?"}`
  );

  // ── New session ──
  logInfo(`Creating session (cwd=${cwd})…`);
  const sessionResult = (await client.request("session/new", {
    cwd,
    mcpServers: [],  // required field per SDK spec
  })) as {
    sessionId: string;
    models?: Array<{ modelId: string; name: string }>;
    modes?: Array<{ modeId: string; title: string }>;
  };

  const sessionId = sessionResult.sessionId;
  client.setSessionId(sessionId);
  logInfo(`Session created: ${C.yellow}${sessionId}${C.reset}`);

  if (sessionResult.models?.length) {
    logInfo(`Available models: ${sessionResult.models.map((m) => m.modelId).join(", ")}`);
  }

  // ── Set model ──
  logInfo(`Setting model to: ${C.yellow}${model}${C.reset}…`);
  try {
    await client.request("session/set_model", { sessionId, modelId: model });
    logInfo(`Model set: ${model}`);
  } catch (e) {
    logError(`session/set_model failed: ${e} — proceeding anyway`);
  }

  // ── Set mode: agent (auto-approve) ──
  if (autoPermission) {
    try {
      await client.request("session/set_mode", { sessionId, modeId: "agent" });
      logInfo("Session mode set to: agent");
    } catch {
      // non-fatal
    }
  }

  // ── REPL ──
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
    terminal: true,
    prompt: `\n${C.bold}${C.cyan}you${C.reset} `,
  });

  rl.prompt();

  let promptRunning = false;

  rl.on("line", async (input) => {
    const line = input.trim();
    if (!line) {
      rl.prompt();
      return;
    }

    // ── Slash commands ──
    if (line.startsWith("/")) {
      const [cmd, ...rest] = line.slice(1).split(/\s+/);
      const arg = rest.join(" ").trim();

      switch (cmd) {
        case "quit":
        case "exit":
          logInfo("Bye!");
          rl.close();
          proc.kill();
          process.exit(0);
          break;

        case "help":
          console.log(`
${C.bold}Commands:${C.reset}
  /quit, /exit         Exit the client
  /model <id>          Switch model for this session
  /mode <agent|ask>    Switch session mode
  /session             Show current session ID
  /sessions            List available sessions
  /personas            List available personas
  /persona <name>      Set persona for current session
  /cancel              Cancel running prompt
`);
          break;

        case "model":
          if (!arg) { logError("Usage: /model <modelId>"); break; }
          try {
            await client.request("session/set_model", { sessionId, modelId: arg });
            logInfo(`Model switched to: ${arg}`);
          } catch (e) {
            logError(`session/set_model: ${e}`);
          }
          break;

        case "mode":
          if (!arg) { logError("Usage: /mode <agent|ask>"); break; }
          try {
            await client.request("session/set_mode", { sessionId, modeId: arg });
            logInfo(`Mode switched to: ${arg}`);
          } catch (e) {
            logError(`session/set_mode: ${e}`);
          }
          break;

        case "session":
          console.log(`${C.bold}Session ID:${C.reset} ${sessionId}`);
          break;

        case "sessions": {
          try {
            const result = (await client.request("session/list", { cwd })) as {
              sessions: Array<{ sessionId: string; title?: string; updatedAt?: string }>;
            };
            console.log(`\n${C.bold}Sessions:${C.reset}`);
            for (const s of result.sessions ?? []) {
              console.log(
                `  ${C.yellow}${s.sessionId}${C.reset}  ${s.title ?? "(no title)"}  ${C.dim}${s.updatedAt ?? ""}${C.reset}`
              );
            }
          } catch (e) {
            logError(`session/list: ${e}`);
          }
          break;
        }

        case "personas": {
          try {
            const result = (await client.request("persona/list", {})) as { personas: string[] };
            console.log(`\n${C.bold}Personas:${C.reset} ${result.personas?.join(", ")}`);
          } catch (e) {
            logError(`persona/list: ${e}`);
          }
          break;
        }

        case "persona":
          if (!arg) { logError("Usage: /persona <name>"); break; }
          try {
            await client.request("persona/set_session", { sessionId, name: arg });
            logInfo(`Persona set to: ${arg}`);
          } catch (e) {
            logError(`persona/set_session: ${e}`);
          }
          break;

        case "cancel":
          await client.notify("session/cancel", { sessionId });
          logInfo("Cancel notification sent");
          break;

        default:
          logError(`Unknown command: /${cmd}  (type /help)`);
      }

      rl.prompt();
      return;
    }

    // ── Send prompt ──
    if (promptRunning) {
      logError("A prompt is already running. Use /cancel to stop it.");
      rl.prompt();
      return;
    }

    promptRunning = true;
    client.resetStreamText();

    try {
      const result = (await client.request("session/prompt", {
        sessionId,
        prompt: [{ type: "text", text: line }],
      })) as { stopReason?: string };

      process.stdout.write("\n");
      logTrace("•", C.dim, `stopReason: ${result?.stopReason ?? "?"}`, "");
    } catch (e) {
      process.stdout.write("\n");
      logError(`prompt error: ${e}`);
    } finally {
      promptRunning = false;
      rl.prompt();
    }
  });

  rl.on("close", () => {
    logInfo("Input closed. Exiting.");
    proc.kill();
    process.exit(0);
  });

  // ── SIGINT / SIGTERM ──
  process.on("SIGINT", () => {
    if (promptRunning) {
      logInfo("Interrupting… sending cancel");
      client.notify("cancel", { sessionId }).catch(() => {});
    } else {
      logInfo("Exiting.");
      proc.kill();
      process.exit(0);
    }
  });

  process.on("SIGTERM", () => {
    proc.kill();
    process.exit(0);
  });
}

main().catch((e) => {
  logError(`Fatal: ${e}`);
  process.exit(1);
});
