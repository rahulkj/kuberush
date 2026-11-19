//go:build !unix

package logs

func fileIDFromSys(sys any) fileID {
	return fileID{}
}
