#!/bin/sh
# Answers with the caller's IP, like api.ipify.org. busybox httpd reports
# IPv4 callers as "[::ffff:a.b.c.d]".
ip=${REMOTE_ADDR#[}
ip=${ip%]}
ip=${ip#::ffff:}
printf 'Content-Type: text/plain\r\n\r\n%s\n' "$ip"
