#!/bin/sh
# Footprint of the running Dictator: resident memory, CPU, idle wakeups,
# process count and app size. Run while it sits idle.
PID=$(pgrep -x Dictator || pgrep -x dictator | head -1)
if [ -z "$PID" ]; then echo "Dictator is not running"; exit 1; fi
echo "pid $PID"
ps -o rss=,%cpu=,time= -p "$PID" | awk '{printf "resident memory : %.1f MB\ncpu now         : %s%%\ncpu time total  : %s\n", $1/1024, $2, $3}'
echo "processes       : $(pgrep -f 'Dictator|dictator' | wc -l | tr -d ' ') (no helpers)"
echo "idle wakeups    : sampling 10 s…"
top -l 2 -s 10 -pid "$PID" -stats pid,cpu,idlew,power | tail -1 | awk '{printf "                  %s idle wakeups in 10 s, cpu %s%%, power %s\n", $3, $2, $4}'
for a in dist/Dictator.app /Applications/Dictator.app; do
	[ -d "$a" ] && echo "app size        : $(du -sh "$a" | cut -f1)  ($a)"
done
