#!/bin/sh
# udhcpc hook: configure eth0 from the lease and note what Batter sent.
case "$1" in
deconfig)
	ip -4 address flush dev "$interface"
	;;
bound | renew)
	ip -4 address flush dev "$interface"
	ip address add "$ip/$mask" dev "$interface"
	ip route replace default via "$router" dev "$interface"
	echo "$ip" > /tmp/ip
	echo "$router" > /tmp/router
	echo "$dns" > /tmp/dns
	echo "nameserver $dns" > /etc/resolv.conf
	;;
esac
exit 0
