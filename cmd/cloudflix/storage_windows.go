package main

import "golang.org/x/sys/windows"

func filesystemSpace(path string) (total, free uint64, err error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	err = windows.GetDiskFreeSpaceEx(name, &free, &total, nil)
	return
}
