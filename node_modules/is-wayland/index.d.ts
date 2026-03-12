/**
Check if the current Linux session is using Wayland.

@example
```
import isWayland from 'is-wayland';

if (isWayland()) {
	console.log('Running in Wayland');
	// Use wl-clipboard tools
} else {
	console.log('Not running in Wayland');
	// Use X11 tools
}
```
*/
export default function isWayland(): boolean;
