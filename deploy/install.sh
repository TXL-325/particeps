#!/bin/sh
set -eu

ROOT=${ROOT:-}
BIN=${1:-./particeps-agent}

if [ "$(uname -m)" != "x86_64" ]; then
  echo "need amd64" >&2
  exit 1
fi
if [ ! -f /etc/os-release ] || ! grep -q 'VERSION_ID="13"' /etc/os-release; then
  echo "need Debian 13" >&2
  exit 1
fi

avail=$(df -BG / | awk 'NR==2 {gsub("G","",$4); print $4}')
if [ "$avail" -lt 16 ]; then
  echo "need at least 16G free on / (have ${avail}G)" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y incus-base incus-client lvm2 thin-provisioning-tools uidmap conntrack

systemctl enable --now incus.service incus.socket || systemctl enable --now incus

if incus storage show particeps-pool >/dev/null 2>&1; then
  echo "storage pool particeps-pool already exists; not touching it"
else
  incus storage create particeps-pool lvm size=16GiB lvm.use_thinpool=true
fi

if incus network show particepsbr0 >/dev/null 2>&1; then
  echo "network particepsbr0 already exists; not touching it"
else
  incus network create particepsbr0 ipv4.address=10.80.0.1/24 ipv4.nat=true ipv6.address=none
fi

if incus project show particeps >/dev/null 2>&1; then
  echo "project particeps already exists; not touching it"
else
  incus project create particeps -c features.images=true -c features.profiles=true -c features.storage.volumes=true
fi

install -d -m 0755 /opt/particeps
install -d -m 0750 /etc/particeps
install -d -m 0700 /var/lib/particeps /var/lib/particeps/backups
install -m 0755 "$BIN" /opt/particeps/particeps-agent
if [ ! -f /etc/particeps/config.yaml ]; then
  install -m 0640 deploy/config.yaml /etc/particeps/config.yaml 2>/dev/null || \
    install -m 0640 "$(dirname "$0")/config.yaml" /etc/particeps/config.yaml
fi
install -m 0644 "$(dirname "$0")/particeps-agent.service" /etc/systemd/system/particeps-agent.service

mkdir -p /sys/fs/cgroup/particeps-guests || true
echo '+cpu' > /sys/fs/cgroup/particeps-guests/cgroup.subtree_control 2>/dev/null || true
echo '150000 100000' > /sys/fs/cgroup/particeps-guests/cpu.max 2>/dev/null || true

systemctl daemon-reload
systemctl enable --now particeps-agent.service
echo "installed. admin password (first start) in /var/lib/particeps/admin-bootstrap.txt"
