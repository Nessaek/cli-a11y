//go:build linux

package run

// fionread is the ioctl that reports how many bytes are waiting to be read.
const fionread = 0x541B
