//go:build unix

package logs

import "syscall"

func fileIDFromSys(sys any) fileID {
	st, ok := sys.(*syscall.Stat_t)
	if !ok || st == nil {
		return fileID{}
	}
	return fileID{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}
}
