//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package run

// fionread is the ioctl that reports how many bytes are waiting to be read.
const fionread = 0x4004667f
