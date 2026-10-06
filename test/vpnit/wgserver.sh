#!/bin/sh
# A minimal WireGuard "VPN provider": one peer ($SUBNET.2), NAT out of eth0.
set -eu
apk add --no-cache -q wireguard-tools-wg iptables
umask 077
# Under /etc/wireguard: hosts with Ubuntu 26.04's AppArmor "wg" profile confine
# /usr/bin/wg even in privileged containers, and it may only open key files there.
mkdir -p /etc/wireguard
echo "$SERVER_KEY" > /etc/wireguard/server.key
ip link add wg0 type wireguard
wg set wg0 listen-port 51820 private-key /etc/wireguard/server.key peer "$CLIENT_PUB" allowed-ips "$SUBNET.2/32"
ip address add "$SUBNET.1/24" dev wg0
ip link set wg0 up
sysctl -qw net.ipv4.ip_forward=1
iptables -t nat -A POSTROUTING -s "$SUBNET.0/24" -o eth0 -j MASQUERADE
touch /ready
exec sleep infinity
