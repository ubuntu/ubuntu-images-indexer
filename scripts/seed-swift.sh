#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT


paths=(
  focal/20.04.6/ubuntu/ubuntu_desktop_20.04.6_amd64_20200418.iso
  focal/20.04.6/ubuntu/ubuntu_live-server_20.04.6_amd64_20200418.iso
  jammy/22.04.5/kubuntu/kubuntu_desktop_22.04.5_amd64_20220418.iso
  jammy/22.04.5/ubuntu/ubuntu_desktop_22.04.5_amd64_20220418.iso
  jammy/22.04.5/ubuntu/ubuntu_live-server_22.04.5_amd64_20220418.iso
  noble/24.04-beta/ubuntu/ubuntu_desktop_24.04-beta_amd64_20240322.iso
  noble/24.04-beta/ubuntu/ubuntu_desktop_24.04-beta_amd64_20240322.iso.sha256
  noble/24.04-beta/ubuntu/ubuntu_desktop_24.04-beta_amd64_20240322.iso.sha256.gpg
  noble/24.04.3/edubuntu/edubuntu_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/lubuntu/lubuntu_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntu-base/ubuntu_base_24.04.3_amd64_20240418.tar.gz
  noble/24.04.3/ubuntu-base/ubuntu_base_24.04.3_arm64_20240418.tar.gz
  noble/24.04.3/ubuntu-budgie/ubuntu-budgie_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntu-mate/ubuntu-mate_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntu-unity/ubuntu-unity_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso.sha256
  noble/24.04.3/ubuntu/ubuntu_desktop_24.04.3_amd64_20240418.iso.sha256.gpg
  noble/24.04.3/ubuntu/ubuntu_desktop_24.04.3_arm64_20240418.iso
  noble/24.04.3/ubuntu/ubuntu_live-server_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntu/ubuntu_live-server_24.04.3_arm64_20240418.iso
  noble/24.04.3/ubuntucinnamon/ubuntucinnamon_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntukylin/ubuntukylin_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/ubuntustudio/ubuntustudio_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/xubuntu/xubuntu_desktop_24.04.3_amd64_20240418.iso
  noble/24.04.3/xubuntu/xubuntu_desktop_24.04.3_amd64_20240418.iso.sha256
  noble/24.04.3/xubuntu/xubuntu_desktop_24.04.3_amd64_20240418.iso.sha256.gpg
  noble/24.04.3/xubuntu/xubuntu_minimal_24.04.3_amd64_20240418.iso
  noble/24.04.3/xubuntu/xubuntu_minimal_24.04.3_amd64_20240418.iso.sha256
  noble/24.04.3/xubuntu/xubuntu_minimal_24.04.3_amd64_20240418.iso.sha256.gpg
  noble/24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20240418.iso
  noble/24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20240418.iso.sha256
  noble/24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20240418.iso.sha256.gpg
  noble/24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20240418.iso.zsync
  noble/24.04.5/kubuntu/kubuntu_desktop_24.04.5_amd64_20240418.iso.manifest
  noble/24.04.5/ubuntu-mini-iso/ubuntu_mini-iso_24.04.5_amd64_20240418.iso
  noble/24.04.5/ubuntu-mini-iso/ubuntu_mini-iso_24.04.5_amd64_20240418.iso.sha256
  noble/24.04.5/ubuntu-mini-iso/ubuntu_mini-iso_24.04.5_amd64_20240418.iso.sha256.gpg
  noble/24.04/ubuntu-core/ubuntu_core_24_amd64_20240419.img.xz
  noble/24.04/ubuntu-core/ubuntu_core_24_arm64_20240419.img.xz
  noble/24.04/ubuntu-server/ubuntu_live-server_24.04_amd64_20240418.iso
  noble/24.04/ubuntu-server/ubuntu_preinstalled-server_24.04_arm64_20240418.img.xz
  noble/24.04/ubuntu-wsl/ubuntu_wsl_24.04_amd64_20240418.wsl
  noble/24.04/ubuntu-wsl/ubuntu_wsl_24.04_arm64_20240418.wsl
  resolute/26.04.1/kubuntu/kubuntu_desktop_26.04.1_amd64_20260418.iso
  resolute/26.04.1/ubuntu/ubuntu_desktop_26.04.1_amd64_20260418.iso
  resolute/26.04.1/ubuntu/ubuntu_desktop_26.04.1_amd64_20260418.iso.sha256
  resolute/26.04.1/ubuntu/ubuntu_desktop_26.04.1_amd64_20260418.iso.sha256.gpg
  resolute/26.04.1/ubuntu/ubuntu_desktop_26.04.1_arm64_20260418.iso
  resolute/26.04.1/ubuntu/ubuntu_desktop_26.04.1_arm64_20260418.iso.sha256
  resolute/26.04.1/ubuntu/ubuntu_desktop_26.04.1_arm64_20260418.iso.sha256.gpg
  resolute/26.04.1/ubuntu/ubuntu_live-server_26.04.1_amd64_20260418.iso
  resolute/26.04.1/ubuntu/ubuntu_live-server_26.04.1_arm64_20260418.iso
  resolute/26.04/ubuntu-base/ubuntu_base_26.04_amd64_20260418.tar.gz
  resolute/26.04/ubuntu-server/ubuntu_live-server_26.04_amd64_20260418.iso
  resolute/26.04/ubuntu-wsl/ubuntu_wsl_26.04_amd64_20260418.wsl
  stonking/26.10/ubuntu/ubuntu_desktop_26.10_amd64_20260418.iso
  stonking/26.10/ubuntu/ubuntu_live-server_26.10_amd64_20260418.iso
  trusty/14.04.6/ubuntu/ubuntu_desktop_14.04.6_amd64_20140412.iso
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
