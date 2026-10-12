#!/bin/sh
set -eu
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
session=keel
command -v tmux >/dev/null 2>&1 || { echo 'Install tmux first' >&2; exit 1; }
if tmux has-session -t "=$session" 2>/dev/null; then
  echo "Session $session exists; attach with: tmux attach -t $session"
  exit 0
fi
for port in 3001 5173; do
  if [ -n "$(ss -H -ltn "sport = :$port")" ]; then
    echo "Port $port is in use; inspect the existing process before starting a session" >&2
    exit 1
  fi
done
tmux new-session -d -s "$session" -n work -c "$project_root"
tmux set-option -t "$session" mouse on
tmux set-option -t "$session" history-limit 20000
tmux set-option -t "$session" status-left '[keel] '
tmux new-window -t "$session:" -n api -c "$project_root" 'make dev-api'
tmux new-window -t "$session:" -n web -c "$project_root" 'make dev-web'
tmux new-window -t "$session:" -n logs -c "$project_root" 'docker compose --profile data logs -f --tail=80'
for window in api web logs; do
  tmux set-window-option -t "$session:$window" remain-on-exit on
done
tmux select-window -t "$session:work"
echo "Session $session ready: work, api(3001), web(5173), logs."
echo "Attach: tmux attach -t $session; detach: Ctrl-b then d."
