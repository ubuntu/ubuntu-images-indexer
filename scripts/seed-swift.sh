#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT


paths=(
  trusty/14.04.6/ubuntu/ubuntu-14.04.6-desktop-amd64.iso
  focal/20.04.6/ubuntu/ubuntu-20.04.6-desktop-amd64.iso
  focal/20.04.6/ubuntu/ubuntu-20.04.6-live-server-amd64.iso
  jammy/22.04.5/ubuntu/ubuntu-22.04.5-desktop-amd64.iso
  jammy/22.04.5/ubuntu/ubuntu-22.04.5-live-server-amd64.iso
  jammy/22.04.5/kubuntu/kubuntu-22.04.5-desktop-amd64.iso
  noble/beta/ubuntu/ubuntu-beta-desktop-amd64.iso
  noble/beta/ubuntu/SHA256SUMS
  noble/beta/ubuntu/SHA256SUMS.gpg
  noble/24.04.3/ubuntu/ubuntu-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntu/ubuntu-24.04.3-desktop-arm64.iso
  noble/24.04.3/ubuntu/ubuntu-24.04.3-live-server-amd64.iso
  noble/24.04.3/ubuntu/ubuntu-24.04.3-live-server-arm64.iso
  noble/24.04.3/ubuntu/SHA256SUMS
  noble/24.04.3/ubuntu/SHA256SUMS.gpg
  noble/24.04.3/ubuntu/HEADER.html
  noble/24.04.3/ubuntu/FOOTER.html
  noble/24.04.5/kubuntu/kubuntu-24.04.5-desktop-amd64.iso
  noble/24.04.5/kubuntu/kubuntu-24.04.5-desktop-amd64.iso.zsync
  noble/24.04.5/kubuntu/kubuntu-24.04.5-desktop-amd64.manifest
  noble/24.04.5/kubuntu/SHA256SUMS
  noble/24.04.3/lubuntu/lubuntu-24.04.3-desktop-amd64.iso
  noble/24.04.3/xubuntu/xubuntu-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntustudio/ubuntustudio-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntukylin/ubuntukylin-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntu-budgie/ubuntu-budgie-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntu-mate/ubuntu-mate-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntucinnamon/ubuntucinnamon-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntu-unity/ubuntu-unity-24.04.3-desktop-amd64.iso
  noble/24.04.3/edubuntu/edubuntu-24.04.3-desktop-amd64.iso
  noble/24.04.3/ubuntu-base/ubuntu-base-24.04.3-base-amd64.tar.gz
  noble/24.04.3/ubuntu-base/ubuntu-base-24.04.3-base-arm64.tar.gz
  noble/24.04.5/ubuntu-mini-iso/ubuntu-mini-iso-24.04.5-mini-iso-amd64.iso
  noble/24.04/ubuntu-wsl/noble-wsl-amd64.wsl
  noble/24.04/ubuntu-wsl/noble-wsl-arm64.wsl
  noble/24.04/ubuntu-server/noble-live-server-amd64.iso
  noble/24.04/ubuntu-server/noble-preinstalled-server-arm64.img.xz
  noble/24.04/ubuntu-core/ubuntu-core-24-amd64.img.xz
  noble/24.04/ubuntu-core/ubuntu-core-24-arm64.img.xz
  resolute/26.04.1/kubuntu/kubuntu-26.04.1-desktop-amd64.iso
  resolute/26.04.1/ubuntu/SHA256SUMS
  resolute/26.04.1/ubuntu/ubuntu-26.04.1-desktop-amd64.iso
  resolute/26.04.1/ubuntu/ubuntu-26.04.1-desktop-arm64.iso
  resolute/26.04.1/ubuntu/ubuntu-26.04.1-live-server-amd64.iso
  resolute/26.04.1/ubuntu/ubuntu-26.04.1-live-server-arm64.iso
  resolute/26.04/ubuntu-base/ubuntu-base-26.04-base-amd64.tar.gz
  resolute/26.04/ubuntu-server/ubuntu-26.04-live-server-amd64.iso
  resolute/26.04/ubuntu-wsl/ubuntu-26.04-wsl-amd64.wsl
  resolute/26.10/ubuntu/ubuntu-26.10-desktop-amd64.iso
  resolute/26.10/ubuntu/ubuntu-26.10-live-server-amd64.iso
)

for path in "${paths[@]}"; do
  size=$(( (RANDOM % 4 + 1) * 512 ))
  echo "$path " >"$tmp"
  head -c "$size" /dev/urandom >>"$tmp"
  container="$(echo "$path" | cut -d'/' -f1)"
  path="$(echo "$path" | cut -d'/' -f2-)"
  swift post "$container"
  swift post "$container" --read-acl ".r:*"
  swift upload "$container" "$tmp" --object-name "$path"
done
