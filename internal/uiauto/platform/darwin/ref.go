package darwin

import "fmt"

// axRef identifies a single live AXUIElementRef within the current
// process: the owning process id plus the raw CFTypeRef pointer value
// (retained in the backend's handle table, released only in Close()).
// Handle is opaque outside this package and is never meaningful across
// process restarts or after the ref has been released.
type axRef struct {
	PID    int32
	Handle uintptr
}

func (r axRef) empty() bool { return r.Handle == 0 }

func (r axRef) String() string {
	return fmt.Sprintf("ax(pid=%d,handle=0x%x)", r.PID, r.Handle)
}

// appProc is a lightweight (pid, process name) pair produced by
// axConn.runningApps, used to build core.AppInfo without touching any AX
// attribute.
type appProc struct {
	PID  int32
	Name string
}
