package main

import (
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

func openFolder(folder string) error {
	path, err := windows.UTF16PtrFromString(folder)
	if err != nil {
		return err
	}
	// Shell extensions may need COM. Keep initialization and the shell call
	// on the same OS thread, including cleanup.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinitApartmentThreaded = 0x2
	const coinitDisableOLE1DDE = 0x4
	err = windows.CoInitializeEx(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	switch err {
	case nil, syscall.Errno(1): // S_OK or S_FALSE: both require cleanup.
		defer windows.CoUninitialize()
	case syscall.Errno(0x80010106): // RPC_E_CHANGED_MODE: COM is already initialized.
	default:
		return err
	}
	return windows.ShellExecute(0, nil, path, nil, nil, windows.SW_SHOWNORMAL)
}
