//go:build linux
// +build linux

package supportos

import "os"

func SetChownItem(name string, uid int, gid int) error {
	err := os.Chown(name, uid, gid)
	if err != nil {
		return err
	}
	return nil
}
