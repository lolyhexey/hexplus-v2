//go:build windows

package ovpnguard

// lockRunDir is a no-op off Linux; the guard only runs on Linux hosts.
func lockRunDir(func(string, ...any)) func() { return func() {} }
