package keystore

import "github.com/godbus/dbus/v5"

// busObject is the part of a remote object the Secret Service backend calls.
type busObject interface {
	Call(method string, flags dbus.Flags, args ...any) *dbus.Call
	GetProperty(p string) (dbus.Variant, error)
}

// busConn is the part of the session bus the Secret Service backend uses. Tests
// replace it with a fake bus.
type busConn interface {
	Object(dest string, path dbus.ObjectPath) busObject
	Signal(ch chan<- *dbus.Signal)
	RemoveSignal(ch chan<- *dbus.Signal)
}
