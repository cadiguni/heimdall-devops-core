package main

import (
	"errors"
	"syscall"
)

// Códigos do Windows para escrita em pipe já fechado. O equivalente ao EPIPE
// do POSIX aparece com um destes dois, dependendo de quando o outro lado sai.
const (
	errorBrokenPipe = syscall.Errno(109) // ERROR_BROKEN_PIPE
	errorNoData     = syscall.Errno(232) // ERROR_NO_DATA
)

func isPlatformBrokenPipe(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errorBrokenPipe || errno == errorNoData
}
