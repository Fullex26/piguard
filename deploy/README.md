# Deployment Helpers

These files are optional host-level helpers and are not installed by the generic PiGuard installer.

## Security scan timer

Install `piguard-security-scan.sh`, its service, and its timer when ClamAV and rkhunter are installed:

```sh
sudo install -o root -g root -m 0755 deploy/piguard-security-scan.sh /usr/local/sbin/piguard-security-scan
sudo install -o root -g root -m 0644 deploy/piguard-security-scan.service /etc/systemd/system/
sudo install -o root -g root -m 0644 deploy/piguard-security-scan.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now piguard-security-scan.timer
```

## Tailscale-bound services

Services that bind directly to a Tailscale address must start after
`tailscale-online.target`, which waits until the interface and address are ready.
Install the matching drop-in only on hosts that use this layout:

```sh
sudo install -d -o root -g root -m 0755 /etc/systemd/system/piguard.service.d
sudo install -o root -g root -m 0644 deploy/piguard-after-tailscale.conf /etc/systemd/system/piguard.service.d/after-tailscale.conf

sudo install -d -o root -g root -m 0755 /etc/systemd/system/docker.service.d
sudo install -o root -g root -m 0644 deploy/docker-after-tailscale.conf /etc/systemd/system/docker.service.d/after-tailscale.conf

sudo systemctl daemon-reload
```

The Docker drop-in is needed only when containers publish ports directly on a Tailscale address.

## Host sysctls

`zz-piguard-hardening.conf` enables stricter FIFO protection and logs malformed or
spoofed IPv4 traffic. Review it for compatibility, then install it with:

```sh
sudo install -o root -g root -m 0644 deploy/zz-piguard-hardening.conf /etc/sysctl.d/zz-piguard-hardening.conf
sudo sysctl --system
```
