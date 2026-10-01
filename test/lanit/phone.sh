#!/bin/sh
# A stand-in phone on the phone network. The test plugs it into the
# "switch" by moving a veth end in as eth0; it then gets an address from
# Batter over DHCP, like a phone with an ethernet adapter, and answers on
# adb's port 5555 so Batter's connections to it can be checked.
# DHCP_HOSTNAME makes it announce a name (the switch's own management
# interface does).
set -eu
until ip link show eth0 >/dev/null 2>&1; do sleep 0.2; done
ip link set eth0 up
nc -lk -p 5555 -e /phone/banner.sh &
exec udhcpc -i eth0 -f -s /phone/udhcpc.sh ${DHCP_HOSTNAME:+-x hostname:$DHCP_HOSTNAME}
