#!/usr/bin/env bash
# Tmux script that sets up a simnet harness.
set -ex
SESSION="tatanka-harness"
ROOT=~/dextest/tatanka
if [ -d "${ROOT}" ]; then
  rm -fR "${ROOT}"
fi
mkdir -p "${ROOT}"

cp "priv.key" "${ROOT}"

cat > "${ROOT}/tatanka.conf" <<EOF
simnet=true
notls=true
EOF

echo "Starting harness"
tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'tatanka'
tmux send-keys -t $SESSION:0 "cd ${ROOT}" C-m
tmux send-keys -t $SESSION:0 "tatanka --appdata=${ROOT} " C-m
tmux attach-session -t $SESSION
