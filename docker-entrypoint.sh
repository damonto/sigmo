#!/bin/sh
set -eu

# Containers do not run OpenRC/systemd, so Sigmo owns these two daemons here.
if [ "${SIGMO_START_DBUS:-1}" = "1" ]; then
	mkdir -p /run/dbus
	rm -f /run/dbus/pid /run/dbus/system_bus_socket
	dbus-daemon --system --nofork --nopidfile &
	dbus_pid=$!

	i=0
	while [ "$i" -lt 40 ]; do
		if dbus-send --system --dest=org.freedesktop.DBus --type=method_call / org.freedesktop.DBus.ListNames >/dev/null 2>&1; then
			break
		fi
		if ! kill -0 "$dbus_pid" 2>/dev/null; then
			echo "D-Bus exited before becoming ready" >&2
			wait "$dbus_pid" || true
			exit 1
		fi
		i=$((i + 1))
		sleep 0.25
	done
	if [ "$i" -eq 40 ]; then
		echo "D-Bus did not become ready" >&2
		exit 1
	fi
fi

if [ "${SIGMO_START_MODEMMANAGER:-1}" = "1" ]; then
	ModemManager &
	modemmanager_pid=$!

	i=0
	while [ "$i" -lt 40 ]; do
		if mmcli -L >/dev/null 2>&1; then
			break
		fi
		if ! kill -0 "$modemmanager_pid" 2>/dev/null; then
			echo "ModemManager exited before becoming ready" >&2
			wait "$modemmanager_pid" || true
			exit 1
		fi
		i=$((i + 1))
		sleep 0.25
	done
	if [ "$i" -eq 40 ]; then
		echo "ModemManager did not become ready" >&2
		exit 1
	fi
fi

if [ "$#" -eq 0 ]; then
	if [ -f /app/config.toml ]; then
		set -- /app/sigmo --config /app/config.toml
	else
		set -- /app/sigmo
	fi
fi

exec "$@"
