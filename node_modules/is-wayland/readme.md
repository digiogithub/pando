# is-wayland

> Check if the current Linux session is using Wayland

## Install

```sh
npm install is-wayland
```

## Usage

```js
import isWayland from 'is-wayland';

if (isWayland()) {
	console.log('Running in Wayland');
	// Use wl-clipboard tools
} else {
	console.log('Not running in Wayland');
	// Use X11 tools
}
```

## How it works

The package detects Wayland sessions by checking:

1. **`WAYLAND_DISPLAY` environment variable** - The primary indicator set by Wayland compositors
2. **`XDG_SESSION_TYPE` environment variable** - Set by display managers to indicate session type

Returns `false` on non-Linux platforms.

## Related

- [is-docker](https://github.com/sindresorhus/is-docker) - Check if the process is running inside a Docker container
- [is-wsl](https://github.com/sindresorhus/is-wsl) - Check if the process is running inside Windows Subsystem for Linux
