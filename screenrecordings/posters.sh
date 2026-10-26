#!/bin/sh
# Write a poster frame (output/<name>.png) for every output/<name>.gif: a
# still to link to the video from the README. The frame is taken from the
# middle of the GIF unless the tape has a "# poster: <seconds>" line.
set -e
cd "$(dirname "$0")"
for gif in output/*.gif; do
	name=$(basename "$gif" .gif)
	at=$(sed -n 's/^# poster: *//p' "$name.tape" 2>/dev/null | head -n 1)
	if [ -z "$at" ]; then
		at=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$gif" | awk '{ print $1 / 2 }')
	fi
	ffmpeg -v error -y -ss "$at" -i "$gif" -frames:v 1 -vf scale=640:-1 "output/$name.png"
	echo "==> output/$name.png (at ${at}s)"
done
