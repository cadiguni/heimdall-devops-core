//go:build !windows

package main

// Fora do Windows o syscall.EPIPE checado em isBrokenPipe já cobre o caso, não
// há código de erro adicional a reconhecer.
func isPlatformBrokenPipe(error) bool { return false }
