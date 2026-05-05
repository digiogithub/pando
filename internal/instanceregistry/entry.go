package instanceregistry

import "time"

// Mode identifies how the instance was launched.
type Mode string

const (
	// ModeTUI identifies a terminal UI instance.
	ModeTUI Mode = "tui"
	// ModeWebUI identifies a web UI instance.
	ModeWebUI Mode = "webui"
	// ModeDesktop identifies a desktop (Wails) instance.
	ModeDesktop Mode = "desktop"
	// ModeACP identifies an ACP server instance.
	ModeACP Mode = "acp"
	// ModeNonInteractive identifies a non-interactive (script/pipe) instance.
	ModeNonInteractive Mode = "noninteractive"
)

// Entry describes a running Pando instance. It is serialized as JSON and
// written to /tmp/pando-instances/<instanceID>.json on startup.
type Entry struct {
	// InstanceID is a UUID that uniquely identifies this process.
	InstanceID string `json:"instance_id"`
	// Path is the canonical absolute path of the working directory.
	Path string `json:"path"`
	// PID is the operating system process ID.
	PID int `json:"pid"`
	// PubPort is the ZMQ PUB port used for broadcasting events.
	PubPort int `json:"pub_port"`
	// RPCPort is the ZMQ ROUTER/RPC port used for request-response communication.
	RPCPort int `json:"rpc_port"`
	// StartedAt is when this instance was started.
	StartedAt time.Time `json:"started_at"`
	// Mode identifies how the instance was launched.
	Mode Mode `json:"mode"`
	// IsPrimary is true if this instance holds the ipc.lock file.
	IsPrimary bool `json:"is_primary"`
}
