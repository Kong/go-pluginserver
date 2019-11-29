#!/bin/bash

if [ "$1" == "-v" ]; then
	VERBOSE='true'
fi
if [ "$1" == "-vv" ]; then
	VERBOSE='very'
fi

pkill go-pluginserver

SOCKET='sock'

if pgrep go-pluginserver -l; then
	PREVIOUS_SERVER="yes"
else
	echo "starting server..."
	[ -S "$SOCKET" ] && rm "$SOCKET"
	./go-pluginserver -socket "$SOCKET" &
	pgrep go-pluginserver -l
fi

msg() {
	query="$1"
	[ -v VERBOSE ] && rq <<< "$query"
	[ "$VERBOSE" == "very" ] && rq <<< "$query" -M | hd
	METHOD="$(rq <<< "$1" -- 'at([2])')"
	response="$(rq <<< "$query" -M | nc -U "$SOCKET" -N | rq -m)"
	[ -v VERBOSE ] && rq <<< "$response"

	ERROR="$(rq <<< "$response" -- 'at [2] ')"
	RESULT="$(rq <<< "$response" -- 'at [3] ')"
}

assert_noerr() {
	if [ "$ERROR" != "null" ]; then
		echo "query: $query"
		echo "response: $response"
		echo "$METHOD : $ERROR" > /dev/stderr
		exit 1
	fi
	echo "$METHOD: ok"
}

assert_fld_match() {
	fld="$1"
	pattern="$2"

	fld_v="$(query_result 'at "'$fld'"')"
	if [[ "$fld_v" =~ "$pattern" ]]; then
		echo "==> $fld_v : ok"
	else
		echo "==> $fld_v : no match '$pattern'"
		exit 1
	fi
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

msg '[0, 19, "plugin.HandleEvent", [{"InstanceId": '$instanceID', "EventName": "access", "Params": [45, 23]}]]'
assert_noerr
eventId=$(query_result 'at "EventId"')

assert_fld_match 'Data.Method' 'kong.request.get_header'
assert_fld_match 'Data.Args' '"host"'

msg '[0, 19, "plugin.Step", [{"EventId": '$eventId', "Data": "example.com"}]]'
assert_noerr
assert_fld_match 'Data.Method' 'kong.response.set_header'
assert_fld_match 'Data.Args[0]' '"x-hello-go"'
assert_fld_match 'Data.Args[1]' '"Go says hello to example.com (/some/where/else/)"'

msg '[0, 19, "plugin.Step", [{"EventId": '$eventId', "Data": "ok"}]]'
assert_noerr
# assert_fld_match 'Data.Method' 'kong.router.get_route'
#
# # msg '[0, 19, "plugin.StepError", [{"EventId": '$eventId', "Data": "not in the mood for routes"}]]'
# msg "$(cat <<-EOF
# [
#   0, 19, "plugin.StepRoute",
#   [{
#     "Data": {
#       "created_at": 1574445198,
#       "https_redirect_status_code": 426,
#       "id": "c0ba987b-99e0-4342-a255-61ff47d54fe6",
#       "paths": [ "/" ],
#       "preserve_host": false,
#       "protocols": [ "http", "https" ],
#       "regex_priority": 0,
#       "service": {
#         "id": "a1a72823-4c75-42b3-92e6-79c865175287"
#       },
#       "strip_path": true,
#       "updated_at": 1574445198
#     },
#     "EventId": 0
#   }]
# ]
# EOF
# )"
# assert_noerr
# # callBack=$(query_result 'at "Data"')
assert_fld_match 'Data' '"ret"'


msg '[0, 19, "plugin.InstanceStatus", ['$instanceID']]'
assert_noerr

msg "[0, 19, \"plugin.CloseInstance\", [$instanceID]]"
assert_noerr


if [ ! -v PREVIOUS_SERVER ]; then
	pkill go-pluginserver
	rm "$SOCKET"
fi

