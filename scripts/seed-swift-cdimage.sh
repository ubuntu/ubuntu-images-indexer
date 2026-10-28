#!/usr/bin/env bash
set -euo pipefail

container="${1:-cdimage}"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

swift post "$container"
swift post "$container" --read-acl ".r:*"

paths=(
  .htaccess
  HEADER.html
  FOOTER.html
  favicon.ico
  ubuntu/releases/14.04.6/release/ubuntu-14.04.6-desktop-amd64.iso
  ubuntu/releases/focal/release/ubuntu-20.04.6-desktop-amd64.iso
  ubuntu/releases/focal/release/ubuntu-20.04.6-live-server-amd64.iso
  ubuntu/releases/focal/release/SHA256SUMS
  ubuntu/releases/jammy/release/ubuntu-22.04.5-desktop-amd64.iso
  ubuntu/releases/jammy/release/ubuntu-22.04.5-live-server-amd64.iso
  ubuntu/releases/jammy/release/SHA256SUMS
  ubuntu/releases/24.04/release/.htaccess
  ubuntu/releases/24.04/release/HEADER.html
  ubuntu/releases/24.04/release/FOOTER.html
  ubuntu/releases/24.04/release/SHA256SUMS
  ubuntu/releases/24.04/release/SHA256SUMS.gpg
  ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso
  ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso.zsync
  ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.iso.torrent
  ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.manifest
  ubuntu/releases/24.04/release/ubuntu-24.04.3-desktop-amd64.list
  ubuntu/releases/24.04/release/ubuntu-24.04.3-live-server-amd64.iso
  ubuntu/releases/24.04/release/ubuntu-24.04.3-live-server-arm64.iso
  ubuntu/releases/noble/release/ubuntu-24.04.5-desktop-amd64.iso
  ubuntu/releases/noble/release/SHA256SUMS
  ubuntu/releases/26.04/release/HEADER.html
  ubuntu/releases/26.04/release/FOOTER.html
  ubuntu/releases/26.04/release/SHA256SUMS
  ubuntu/releases/26.04/release/ubuntu-26.04-desktop-amd64.iso
  ubuntu/releases/26.04/release/ubuntu-26.04-desktop-arm64.iso
  ubuntu/releases/26.04/release/ubuntu-26.04-live-server-amd64.iso
  ubuntu/releases/26.04/release/ubuntu-26.04-live-server-arm64.iso
  kubuntu/releases/noble/release/kubuntu-24.04.5-desktop-amd64.iso
  kubuntu/releases/noble/release/kubuntu-24.04.5-desktop-amd64.iso.zsync
  kubuntu/releases/noble/release/kubuntu-24.04.5-desktop-amd64.iso.torrent
  kubuntu/releases/noble/release/kubuntu-24.04.5-desktop-amd64.manifest
  kubuntu/releases/noble/release/SHA256SUMS
  lubuntu/releases/noble/release/lubuntu-24.04.3-desktop-amd64.iso
  xubuntu/releases/noble/release/xubuntu-24.04.3-desktop-amd64.iso
  ubuntustudio/releases/noble/release/ubuntustudio-24.04.3-desktop-amd64.iso
  ubuntukylin/releases/noble/release/ubuntukylin-24.04.3-desktop-amd64.iso
  ubuntu-budgie/releases/noble/release/ubuntu-budgie-24.04.3-desktop-amd64.iso
  ubuntu-mate/releases/noble/release/ubuntu-mate-24.04.3-desktop-amd64.iso
  ubuntucinnamon/releases/noble/release/ubuntucinnamon-24.04.3-desktop-amd64.iso
  ubuntu-unity/releases/noble/release/ubuntu-unity-24.04.3-desktop-amd64.iso
  edubuntu/releases/noble/release/edubuntu-24.04.3-desktop-amd64.iso
  ubuntu-base/releases/noble/release/.htaccess
  ubuntu-base/releases/noble/release/HEADER.html
  ubuntu-base/releases/noble/release/FOOTER.html
  ubuntu-base/releases/noble/release/SHA256SUMS
  ubuntu-base/releases/noble/release/ubuntu-base-24.04.3-base-amd64.tar.gz
  ubuntu-base/releases/noble/release/ubuntu-base-24.04.3-base-arm64.tar.gz
  ubuntu-mini-iso/releases/noble/release/SHA256SUMS
  ubuntu-mini-iso/releases/noble/release/ubuntu-mini-iso-24.04.5-mini-iso-amd64.iso
  ubuntu-mini-iso/releases/noble/release/ubuntu-mini-iso-24.04.5-mini-iso-amd64.iso.zsync
  ubuntu-mini-iso/releases/noble/release/ubuntu-mini-iso-24.04.5-mini-iso-amd64.iso.torrent
  ubuntu-server/noble/daily-live/current/noble-live-server-amd64.iso
  ubuntu-server/noble/daily-live/current/noble-live-server-arm64.iso
  ubuntu-server/noble/daily-live/current/noble-netboot-amd64.tar.gz
  ubuntu-server/noble/daily-live/current/SHA256SUMS
  ubuntu-server/noble/daily-live/current/SHA256SUMS.gpg
  ubuntu-server/noble/daily-preinstalled/current/noble-preinstalled-server-amd64.img.xz
  ubuntu-server/noble/daily-preinstalled/current/noble-preinstalled-server-arm64+raspi.img.xz
  ubuntu-server/resolute/daily-live/current/resolute-live-server-amd64.iso
  ubuntu-server/resolute/daily-live/current/resolute-live-server-arm64.iso
  ubuntu-server/resolute/daily-live/current/SHA256SUMS
  ubuntu-wsl/noble/daily-live/current/noble-wsl-amd64.wsl
  ubuntu-wsl/noble/daily-live/current/noble-wsl-arm64.wsl
  ubuntu-wsl/noble/daily-live/current/noble-wsl-amd64.manifest
  ubuntu-wsl/noble/daily-live/current/SHA256SUMS
  ubuntu-wsl/resolute/daily-live/current/resolute-wsl-amd64.wsl
  ubuntu-wsl/resolute/daily-live/current/resolute-wsl-arm64.wsl
  ubuntu-wsl/resolute/daily-live/current/SHA256SUMS
  ubuntu-core/24/stable/20261015.1/HEADER.html
  ubuntu-core/24/stable/20261015.1/FOOTER.html
  ubuntu-core/24/stable/20261015.1/SHA256SUMS
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.img.xz
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.img.xz.zsync
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.qcow2
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.manifest
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.model-assertion
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-amd64.lxd.tar.xz
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-arm64.img.xz
  ubuntu-core/24/stable/20261015.1/ubuntu-core-24-arm64.qcow2
  ubuntu-core/26/stable/20261025.1/SHA256SUMS
  ubuntu-core/26/stable/20261025.1/ubuntu-core-26-amd64.img.xz
  netboot/index.html
  netboot/focal/index.html
)

for path in "${paths[@]}"; do
  size=$(( (RANDOM % 4 + 1) * 512 ))
  echo "$path " >"$tmp"
  head -c "$size" /dev/urandom >>"$tmp"
  swift upload "$container" "$tmp" --object-name "$path"
done
