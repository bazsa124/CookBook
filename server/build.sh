#!/bin/sh
# Cross-compile the server and importer for every supported host into dist/.
# Pure Go (no cgo), so this works from any OS with Go installed.
set -eu
cd "$(dirname "$0")"
VERSION="${VERSION:-0.1.0}"
mkdir -p dist
for target in linux/amd64 linux/arm64 linux/386 windows/amd64; do
	os="${target%/*}"
	arch="${target#*/}"
	ext=""
	[ "$os" = windows ] && ext=".exe"
	for cmd in cookbook cookbook-import; do
		echo "building $cmd $os/$arch"
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
			-ldflags "-s -w -X main.version=$VERSION" \
			-o "dist/$cmd-$os-$arch$ext" "./cmd/$cmd"
	done
done
