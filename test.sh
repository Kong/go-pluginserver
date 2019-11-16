#!/bin/bash

SOCKET='sock'

if pgrep go-pluginserver -l; then
	PREVIOUS_SERVER="yes"
else
	echo "starting server..."
	./go-pluginserver -socket "$SOCKET" &
	pgrep go-pluginserver -l
fi

msg() {
	METHOD="$(rq <<< "$1" -- 'at([2])')"
	response="$(rq <<< "$1" -M | nc -U "$SOCKET" -N | rq -m)"

	ERROR="$(rq <<< "$response" -- 'at [2] ')"
	RESULT="$(rq <<< "$response" -- 'at [3] ')"
}

assert_noerr() {
	if [ "$ERROR" != "null" ]; then
		echo "$METHOD : $ERROR" > /dev/stderr
		exit 1
	fi
	echo "$METHOD: ok"
}

query_result() {
# 	echo "$RESULT"
	rq <<< "$RESULT" -- "$1"
}

msg '[0, 19, "plugin.SetPluginDir", ["/home/javier/devel/kong_dev/kong"]]'
assert_noerr

msg '[0, 19, "plugin.GetPluginInfo", ["go-log"]]'
assert_noerr

msg '[0, 19, "plugin.StartInstance", [{"Name":"go-log", "Config":"{\"reopen\":false, \"path\":\"/some/where/else/\"}"}]]'
assert_noerr
instanceID=$(query_result 'at "Id"')
echo "instanceID: $instanceID"

msg "[0, 19, \"plugin.CloseInstance\", [$instanceID]]"
assert_noerr


if [ ! -v PREVIOUS_SERVER ]; then
	pkill go-pluginserver
	rm "$SOCKET"
fi

