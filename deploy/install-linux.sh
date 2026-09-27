#!/bin/sh
# Install CookBook as a systemd service on a Linux host (the NAS laptop).
#
#   sudo ./install-linux.sh path/to/cookbook-linux-amd64 [path/to/cookbook-import-linux-amd64]
#
# Idempotent: re-run it to upgrade the binary. Data in /var/lib/cookbook is
# never touched.
set -eu

BIN="${1:?usage: install-linux.sh <cookbook binary> [importer binary]}"
IMPORT="${2:-}"
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)"; exit 1; }

id cookbook >/dev/null 2>&1 || useradd --system --home-dir /var/lib/cookbook --shell /usr/sbin/nologin cookbook
install -d -o cookbook -g cookbook -m 0750 /var/lib/cookbook
install -m 0755 "$BIN" /usr/local/bin/cookbook
[ -n "$IMPORT" ] && install -m 0755 "$IMPORT" /usr/local/bin/cookbook-import

if [ ! -f /etc/default/cookbook ]; then
	cat > /etc/default/cookbook <<'EOF'
# Environment for the CookBook service. Restart after editing:
#   sudo systemctl restart cookbook
COOKBOOK_DATA=/var/lib/cookbook
# Auto-translate: set one of these (free tiers are enough).
#GEMINI_API_KEY=
#GROQ_API_KEY=
# Default listens on loopback + Tailscale only. Uncomment to override:
#COOKBOOK_LISTEN=0.0.0.0:8738
EOF
	chmod 0640 /etc/default/cookbook
	chgrp cookbook /etc/default/cookbook
fi

cat > /etc/systemd/system/cookbook.service <<'EOF'
[Unit]
Description=CookBook recipe server
# Tailscale may come up later; the server re-scans for its address every
# minute, so ordering is a nicety, not a requirement.
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
User=cookbook
Group=cookbook
EnvironmentFile=-/etc/default/cookbook
ExecStart=/usr/local/bin/cookbook
Restart=on-failure
RestartSec=5
# Hardening: the server only needs its data directory.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/cookbook
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now cookbook
systemctl restart cookbook

command -v typst >/dev/null 2>&1 || echo "NOTE: typst not found on PATH - PDFs are disabled. Install it (e.g. 'cargo install typst-cli' or a release binary into /usr/local/bin) and restart."
command -v ffmpeg >/dev/null 2>&1 || echo "NOTE: ffmpeg not found - uploaded videos play, but without a preview frame. Install with: sudo apt install ffmpeg"
command -v tailscale >/dev/null 2>&1 && echo "Tailscale address: $(tailscale ip -4 2>/dev/null | head -n1)"
echo "API token: $(cat /var/lib/cookbook/token 2>/dev/null || echo '(created on first start: sudo cat /var/lib/cookbook/token)')"
echo "Logs: journalctl -u cookbook -f"
