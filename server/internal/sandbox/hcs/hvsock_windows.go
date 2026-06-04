//go:build windows

package hcs

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	afHyperV      = 34
	sockStream    = 1
	hvProtocolRaw = 1
)

type sockaddrHv struct {
	Family    uint16
	Reserved  uint16
	VMID      windows.GUID
	ServiceID windows.GUID
}

var (
	ws2_32              = windows.NewLazySystemDLL("Ws2_32.dll")
	procConnect         = ws2_32.NewProc("connect")
	procRecv            = ws2_32.NewProc("recv")
	procSend            = ws2_32.NewProc("send")
	procWSAGetLastError = ws2_32.NewProc("WSAGetLastError")
)

func connectHVSock(socket windows.Handle, vmID, serviceID windows.GUID) error {
	addr := sockaddrHv{Family: afHyperV, VMID: vmID, ServiceID: serviceID}
	r1, _, err := procConnect.Call(uintptr(socket), uintptr(unsafe.Pointer(&addr)), uintptr(sizeOf[sockaddrHv]()))
	if int32(r1) == -1 {
		if err != windows.ERROR_SUCCESS {
			return err
		}
		return wsaGetLastError()
	}
	return nil
}

func recvHVSock(socket windows.Handle, b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	r1, _, err := procRecv.Call(uintptr(socket), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0)
	if int32(r1) == -1 {
		if err != windows.ERROR_SUCCESS {
			return 0, err
		}
		return 0, wsaGetLastError()
	}
	return int(r1), nil
}

func sendHVSock(socket windows.Handle, b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	r1, _, err := procSend.Call(uintptr(socket), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0)
	if int32(r1) == -1 {
		if err != windows.ERROR_SUCCESS {
			return 0, err
		}
		return 0, wsaGetLastError()
	}
	return int(r1), nil
}

func wsaGetLastError() error {
	r1, _, _ := procWSAGetLastError.Call()
	return windows.Errno(r1)
}
