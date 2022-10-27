# commands

```
mkdir ~/.certs
openssl req -new -newkey rsa:4096 -x509 -sha256 -days 3650 -nodes -out ~/.certs/local.crt -keyout ~/.certs/local.key -subj "/C=US/O=rclone/OU=rclone-dev/CN=dev.rclone.org"

rm -f /tmp/rclone.sock

go run rclone.go rcd2 --rcd2-addrs /tmp/rclone.sock --rcd2-addrs localhost:1234 --rcd2-addrs tls://127.0.0.1:8443 --rcd2-cert ~/.certs/local.crt --rcd2-key ~/.certs/local.key

curl --unix-socket /tmp/rclone.sock http://localhost/core/stats
curl http://localhost:1234/core/stats
curl -k https://localhost:8443/core/stats
```