#!/bin/sh
# A stand-in phone on the phone LAN: it drops the address Docker gave it
# and gets one from Batter over DHCP, like a phone with an ethernet adapter,
# and answers on adb's port 5555 so Batter's connections to it can be
# checked.
set -eu
ip -4 address flush dev eth0
ip -6 address flush dev eth0 scope global 2>/dev/null || true
nc -lk -p 5555 -e /phone/banner.sh &
exec udhcpc -i eth0 -f -s /phone/udhcpc.sh
