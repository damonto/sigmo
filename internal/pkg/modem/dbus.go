package modem

import (
	"context"
	"errors"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	dbusErrUnknownObject = "org.freedesktop.DBus.Error.UnknownObject"
	dbusErrCanceled      = "org.freedesktop.DBus.Error.Canceled"
	dbusErrCancelled     = "org.freedesktop.DBus.Error.Cancelled"
	dbusPropertiesGet    = "org.freedesktop.DBus.Properties.Get"
)

func systemBusObject(objectPath dbus.ObjectPath) (dbus.BusObject, error) {
	dbusConn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	return dbusConn.Object(ModemManagerInterface, objectPath), nil
}

func systemBusPrivate() (*dbus.Conn, error) {
	dbusConn, err := dbus.SystemBusPrivate()
	if err != nil {
		return nil, err
	}
	if err := dbusConn.Auth(nil); err != nil {
		dbusConn.Close()
		return nil, err
	}
	if err := dbusConn.Hello(); err != nil {
		dbusConn.Close()
		return nil, err
	}
	return dbusConn, nil
}

func dbusProperty(ctx context.Context, object dbus.BusObject, iface string, property string) (dbus.Variant, error) {
	if iface == "" || property == "" {
		return dbus.Variant{}, errors.New("dbus property name is invalid")
	}
	var variant dbus.Variant
	err := object.CallWithContext(ctx, dbusPropertiesGet, 0, iface, property).Store(&variant)
	return variant, err
}

func isUnknownObjectError(err error) bool {
	var dbusErr dbus.Error
	if errors.As(err, &dbusErr) {
		if dbusErr.Name == dbusErrUnknownObject {
			return true
		}
	}
	var dbusErrPtr *dbus.Error
	if errors.As(err, &dbusErrPtr) && dbusErrPtr != nil {
		if dbusErrPtr.Name == dbusErrUnknownObject {
			return true
		}
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "object does not exist at path") || strings.Contains(message, "unknown object")
}

func isCanceledError(err error) bool {
	var dbusErr dbus.Error
	if errors.As(err, &dbusErr) {
		if isCanceledName(dbusErr.Name) {
			return true
		}
	}
	var dbusErrPtr *dbus.Error
	if errors.As(err, &dbusErrPtr) && dbusErrPtr != nil {
		if isCanceledName(dbusErrPtr.Name) {
			return true
		}
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "operation was cancelled") || strings.Contains(message, "operation was canceled")
}

func isTransientRestartError(err error) bool {
	if err == nil {
		return false
	}
	return isUnknownObjectError(err) || isCanceledError(err)
}

// IsTransientRestartError reports whether a DBus call likely raced with modem re-enumeration.
func IsTransientRestartError(err error) bool {
	return isTransientRestartError(err)
}

func isCanceledName(name string) bool {
	switch name {
	case dbusErrCanceled, dbusErrCancelled:
		return true
	default:
		return strings.Contains(strings.ToLower(name), "cancel")
	}
}
