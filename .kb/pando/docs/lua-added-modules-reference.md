# Pando Lua Engine — Added GopherLua Modules Reference

> Last updated: 2026-05-31  
> Scope: newly enabled Lua modules added to Pando's embedded Lua engine  
> Source packages: `github.com/vadv/gopher-lua-libs/{cmd,tcp,shellescape}` and `github.com/otm/gluash`

This document describes the newly enabled modules that are now available from Lua scripts executed by Pando.

---

## Overview

The following modules were added to the embedded Lua runtime:

- `cmd`
- `tcp`
- `shellescape`
- `sh`

All of them are **preloaded** in the Lua state, so they can be imported with `require(...)` directly:

```lua
local cmd = require("cmd")
local tcp = require("tcp")
local shellescape = require("shellescape")
local sh = require("sh")
```

---

## 1. `cmd` module

**Purpose**: run a shell command and capture `stdout`, `stderr`, and exit status in one result table.

### Available functions

#### `cmd.exec(command, timeout_seconds?)`

Execute a command string using the host shell:
- on Linux/macOS: `sh -c <command>`
- on Windows: `cmd.exe /C <command>`

### Parameters

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `command` | `string` | yes | Full shell command to execute |
| `timeout_seconds` | `number` | no | Timeout in seconds, default `10` |

### Return values

Success returns one value:
- `result` table with:
  - `status` → numeric exit code (`0` means success)
  - `stdout` → captured standard output
  - `stderr` → captured standard error

Failure/timeout returns two values:
- `nil`
- error string

### Example: successful command

```lua
local cmd = require("cmd")

local result, err = cmd.exec("echo hello", 5)
if err then
  error(err)
end

print(result.status)   -- 0
print(result.stdout)   -- hello\n
print(result.stderr)   -- usually empty
```

### Example: command with non-zero exit code

```lua
local cmd = require("cmd")

local result, err = cmd.exec("ls /path/that/does/not/exist", 5)
if err then
  error(err) -- startup/timeout/system-level error
end

print("status", result.status)
print("stdout", result.stdout)
print("stderr", result.stderr)
```

### Example: timeout

```lua
local cmd = require("cmd")

local result, err = cmd.exec("sleep 30", 1)
if err then
  print("command failed:", err) -- execute timeout
end
```

### Notes

- `cmd.exec` is the simplest API when you want a single command execution with captured output.
- It does **not** stream output incrementally; it waits for completion or timeout.
- Use `shellescape` when interpolating user input into the command string.

---

## 2. `tcp` module

**Purpose**: open a raw TCP connection, write bytes/strings, read responses, and close the socket.

### Available functions

#### `tcp.open(address, dial_timeout_seconds?)`

Open a TCP connection to `host:port`.

### Parameters

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `address` | `string` | yes | TCP target in `host:port` format |
| `dial_timeout_seconds` | `number` | no | Dial timeout in seconds, default `5` |

### Return values

Success:
- `conn` userdata (`tcp_client_ud`)

Failure:
- `nil`
- error string

### Connection methods

A connection object returned by `tcp.open(...)` exposes these methods:

#### `conn:write(data)`

Write data to the socket.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `data` | `string` | yes | Raw payload to send |

Returns:
- usually `nil` on success from the underlying writer wrapper
- error on failure depending on the wrapper behavior

#### `conn:read(max_size?)`

Read up to `max_size` bytes from the socket.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `max_size` | `number` | no | Maximum bytes to read, default `1024` |

Returns:
- `string` data
- optional error

#### `conn:close()`

Close the connection.

### Configurable timeout fields on the connection

The TCP userdata also exposes mutable timeout fields in seconds:
- `dialTimeout`
- `writeTimeout`
- `readTimeout`
- `closeTimeout`

### Example: simple HTTP request over raw TCP

```lua
local tcp = require("tcp")

local conn, err = tcp.open("example.com:80", 5)
if err then
  error(err)
end

conn.writeTimeout = 2
conn.readTimeout = 2

local writeErr = conn:write("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")
if writeErr then
  error(writeErr)
end

local body, readErr = conn:read(4096)
if readErr then
  error(readErr)
end

print(body)
conn:close()
```

### Example: customize timeouts

```lua
local tcp = require("tcp")

local conn, err = tcp.open("127.0.0.1:9000", 1)
if err then
  error(err)
end

conn.writeTimeout = 5
conn.readTimeout = 10

conn:write("ping")
local reply = conn:read(64)
print(reply)
conn:close()
```

### Notes

- This is raw TCP, not HTTP/TLS-aware by itself.
- If you need HTTP-level semantics, prefer the `http` module.
- Reads are bounded by the requested size and the configured read timeout.

---

## 3. `shellescape` module

**Purpose**: safely quote shell arguments or command arrays before passing them to a shell-based executor.

### Available functions

#### `shellescape.quote(value)`

Quote a single shell argument.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `value` | `string` | yes | Raw argument to escape |

Returns:
- escaped string

#### `shellescape.quote_command(args)`

Quote a full command represented as an array of strings.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `args` | `table` | yes | Lua array of command parts |

Returns:
- single shell-safe command string

#### `shellescape.strip_unsafe(value)`

Strip characters considered unsafe by the underlying library.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `value` | `string` | yes | Input string |

Returns:
- sanitized string

### Example: quote one value

```lua
local shellescape = require("shellescape")

local value = "hello world; rm -rf /"
local escaped = shellescape.quote(value)
print(escaped)
```

### Example: build a safe shell command for `cmd.exec`

```lua
local cmd = require("cmd")
local shellescape = require("shellescape")

local filename = "my file.txt"
local command = shellescape.quote_command({"cat", filename})

local result, err = cmd.exec(command, 5)
if err then
  error(err)
end

print(result.stdout)
```

### Example: strip unsafe characters

```lua
local shellescape = require("shellescape")

local cleaned = shellescape.strip_unsafe("hello $(dangerous) world")
print(cleaned)
```

### Notes

- `shellescape` does **not** execute anything.
- It is most useful together with `cmd` or `sh`.
- Prefer `quote_command({...})` over manual string concatenation.

---

## 4. `sh` module

**Purpose**: run external programs as chained Lua objects, with support for shell-like command composition, pipes, output inspection, and exit-code helpers.

This module is provided by `gluash` and behaves differently from `cmd`:
- `cmd.exec(...)` executes a whole shell command string and returns a result table.
- `sh` builds command objects and supports chaining/piping methods.

### Import

```lua
local sh = require("sh")
```

### Main ways to create commands

#### A. `sh.command_name(arg1, arg2, ...)`

Call a command as if it were a method on the module:

```lua
local sh = require("sh")
sh.echo("hello", "world"):print()
```

#### B. `sh(path, arg1, arg2, ...)`

Call a command by explicit path/name, useful for reserved or exotic command names:

```lua
local sh = require("sh")
sh("/bin/echo", "hello"):print()
```

#### C. `sh{ ...config... }`

Configure module-wide behavior.

Supported keys:
- `abort` → `boolean`
- `wait` → `boolean`
- `debug` → `boolean`

Example:

```lua
local sh = require("sh")
sh{abort=true, wait=true}
```

Reading the current config:

```lua
local sh = require("sh")
local cfg = sh{}
print(cfg.abort)
print(cfg.wait)
print(cfg.debug)
```

### Arguments

Each argument must be passed as a separate string.

Correct:

```lua
sh.ls("-la", "/tmp")
```

Incorrect:

```lua
sh.ls("-la /tmp")
```

---

## 4.1 Piping commands

Commands can be chained to pipe stdout from one command into the next.

### Example: pipe to another normal command

```lua
local sh = require("sh")
sh.echo("alpha\nbeta\nalpha"):grep("alpha"):print()
```

### Example: pipe using explicit command name with `:cmd(...)`

Use this when the next command name is dynamic or awkward.

```lua
local sh = require("sh")
sh.echo("alpha\nbeta\nalpha"):cmd("grep", "alpha"):print()
```

#### `command:cmd(path, arg1, arg2, ...)`

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `path` | `string` | yes | Executable to run in the next pipe stage |
| `arg1...` | `string` | no | Command arguments |

Returns:
- next shell command userdata

---

## 4.2 Output and execution helpers on `sh` commands

A `sh` command object exposes the following helpers.

### `command:print()`

Print combined stdout + stderr to the process stdout and wait for completion.

Example:

```lua
local sh = require("sh")
sh.echo("hello"):print()
```

### `command:ok()`

Wait for completion and raise a Lua error if exit status is not zero.
Returns the same command object on success.

Example:

```lua
local sh = require("sh")
sh.echo("hello"):ok():print()
```

### `command:success()`

Wait for completion and return `true` if exit code is `0`, otherwise `false`.

Example:

```lua
local sh = require("sh")
local ok = sh.echo("hello"):success()
print(ok)
```

### `command:exitcode()`

Wait for completion and return the numeric exit code.

Example:

```lua
local sh = require("sh")
local code = sh.echo("hello"):exitcode()
print(code)
```

---

## 4.3 Reading output from `sh`

### `command:stdout(filename?)`

Return stdout as a string. If `filename` is provided, also write it to that file.

### `command:stderr(filename?)`

Return stderr as a string. If `filename` is provided, also write it to that file.

### `command:combinedOutput(filename?)`

Return stdout + stderr combined as a string. If `filename` is provided, also write it to that file.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `filename` | `string` | no | Output file path; file is truncated if it exists |

Example:

```lua
local sh = require("sh")
local output = sh.echo("hello world"):combinedOutput()
print(output)
```

Example writing to a file:

```lua
local sh = require("sh")
local output = sh.echo("hello world"):stdout("/tmp/output.txt")
print(output)
```

### `command:lines(stream?)`

Return an iterator over output lines.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `stream` | `string` | no | Either `"stdout"` or `"stderr"`; default `"stdout"` |

Example:

```lua
local sh = require("sh")
for line in sh.echo("one\ntwo\nthree"):lines() do
  print(line)
end
```

Example reading stderr lines:

```lua
local sh = require("sh")
for line in sh.sh("-c", "echo problem 1>&2"):lines("stderr") do
  print(line)
end
```

---

## 4.4 Glob helper

### `sh.glob(pattern)`

Expand a filesystem glob pattern and return matches.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `pattern` | `string` | yes | Glob expression |

Example:

```lua
local sh = require("sh")
sh.ls(sh.glob("*.go")):print()
```

---

## Choosing between `cmd` and `sh`

### Use `cmd` when:
- you already have a shell command string
- you want a single synchronous execution
- you want `{status, stdout, stderr}` returned directly

### Use `sh` when:
- you want composable pipelines
- you want to inspect output incrementally
- you want `ok()`, `success()`, `exitcode()`, `lines()`, or `combinedOutput()` helpers

### Use `shellescape` with both when:
- user or dynamic input is interpolated into command arguments
- you want to reduce quoting mistakes in shell-based execution

---

## Practical examples

### Example: safely cat a filename from user input

```lua
local cmd = require("cmd")
local shellescape = require("shellescape")

local filename = "report final.txt"
local command = shellescape.quote_command({"cat", filename})

local result, err = cmd.exec(command, 5)
if err then
  error(err)
end

if result.status ~= 0 then
  error(result.stderr)
end

print(result.stdout)
```

### Example: shell-style pipeline with `sh`

```lua
local sh = require("sh")

local output = sh.echo("orange\napple\norange\npear")
  :sort()
  :cmd("uniq")
  :combinedOutput()

print(output)
```

### Example: raw TCP request to a local service

```lua
local tcp = require("tcp")

local conn, err = tcp.open("127.0.0.1:8080", 2)
if err then
  error(err)
end

conn:write("PING\n")
local reply, readErr = conn:read(128)
if readErr then
  error(readErr)
end

print(reply)
conn:close()
```

---

## Security note

These modules increase the power of Lua hooks substantially:
- `cmd` and `sh` can execute system commands
- `tcp` can open outbound network connections
- `shellescape` helps build safer shell strings, but does not make arbitrary command execution safe by itself

When documenting or using these modules in hooks, prefer:
- strict input validation
- explicit allowlists of commands/hosts where possible
- `shellescape.quote_command({...})` instead of manual concatenation
- timeouts for command execution
