#!/bin/sh
# Demo stand-in for "systemctl start nginx": on web02 the first attempt
# fails because port 80 is still held by the old process; the retry works.
state="$1/run/nginx.started"
host=$(basename "$1")
if [ "$host" = web02 ] && [ ! -e "$state" ]; then
  touch "$state"
  echo "nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)" >&2
  exit 1
fi
touch "$state"
