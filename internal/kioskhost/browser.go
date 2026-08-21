package kioskhost

import "sync"

var browserState struct {
	sync.RWMutex
	pid uint32
}

func RecordBrowserPID(pid int) {
	if pid < 1 {
		return
	}
	browserState.Lock()
	browserState.pid = uint32(pid)
	browserState.Unlock()
}

func browserPID() uint32 {
	browserState.RLock()
	defer browserState.RUnlock()
	return browserState.pid
}
