import process from 'node:process';

export default function isWayland() {
	if (process.platform !== 'linux') {
		return false;
	}

	// Primary indicator: WAYLAND_DISPLAY environment variable
	// This variable specifies the Wayland display socket and is the most
	// reliable indicator of a Wayland session according to the Wayland protocol
	if (process.env.WAYLAND_DISPLAY) {
		return true;
	}

	// Secondary indicator: XDG_SESSION_TYPE
	// Set by display managers (GDM, SDDM, LightDM) to indicate session type
	// May not be present in all environments but is a standard freedesktop.org variable
	if (process.env.XDG_SESSION_TYPE === 'wayland') {
		return true;
	}

	return false;
}
