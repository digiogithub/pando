**This package is a temporary fork of `@lvce-editor/ripgrep`**

# ripgrep

A module for using [ripgrep](https://github.com/BurntSushi/ripgrep/) in a Node project.

Same as [vscode-ripgrep](https://github.com/microsoft/vscode-ripgrep), but fixes the github rate limiting error `Downloading ripgrep failed: Error: Request failed: 403` by downloading the files directly instead of also using the github rest api.

## Install

```
$ npm install @lvce-editor/ripgrep
```

## Usage

```js
import { rgPath } = from "@lvce-editor/ripgrep"
import { spawn } from 'node:child_process'

const childProcess = spawn(rgPath, ["abc", "."], {
  stdio: "inherit",
});
```

## Gitpod

[![Open in Gitpod](https://gitpod.io/button/open-in-gitpod.svg)](https://gitpod.io#https://github.com/lvce-editor/ripgrep)

## Credits

This project is very much based on https://github.com/microsoft/vscode-ripgrep by Microsoft.
