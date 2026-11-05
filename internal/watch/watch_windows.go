package watch

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// watchTree uses ReadDirectoryChangesW with subtree watching: one handle per
// profile, the goroutine sleeps in the kernel until something changes.
func watchTree(dir string, emit func(string)) (func(), error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}

	var stopped atomic.Bool
	go func() {
		defer windows.CloseHandle(h)
		buf := make([]byte, 64*1024) // must stay DWORD-aligned; Go allocations are
		const mask = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
			windows.FILE_NOTIFY_CHANGE_DIR_NAME |
			windows.FILE_NOTIFY_CHANGE_SIZE |
			windows.FILE_NOTIFY_CHANGE_LAST_WRITE
		for {
			var n uint32
			err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, mask, &n, nil, 0)
			if stopped.Load() {
				return
			}
			if errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) || (err == nil && n == 0) {
				emit(Rescan) // kernel buffer overflowed
				continue
			}
			if err != nil {
				return // directory deleted or handle invalid
			}
			for off := uint32(0); ; {
				info := (*windows.FileNotifyInformation)(unsafe.Pointer(&buf[off]))
				name := unsafe.Slice(&info.FileName, info.FileNameLength/2)
				emit(filepath.Join(dir, windows.UTF16ToString(name)))
				if info.NextEntryOffset == 0 {
					break
				}
				off += info.NextEntryOffset
			}
		}
	}()

	return func() {
		stopped.Store(true)
		_ = windows.CancelIoEx(h, nil)
	}, nil
}
