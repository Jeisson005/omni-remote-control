#!/usr/bin/env bash
set -e

echo "[TEST-CLIENT] Starting virtual X11 display with Xvfb on :99..."
Xvfb :99 -screen 0 1280x800x24 -ac &
XVFB_PID=$!

export DISPLAY=:99

# Wait a moment for Xvfb
sleep 1

echo "[TEST-CLIENT] Starting openbox window manager..."
openbox &
WM_PID=$!

sleep 1

echo "[TEST-CLIENT] Launching a sample GUI window (xterm)..."
xterm -title "OmniTerminalWindow" -geometry 80x24+50+50 &
XTERM_PID=$!

sleep 1

echo "[TEST-CLIENT] Starting Omni Agent..."
exec /usr/local/bin/omni-agent
