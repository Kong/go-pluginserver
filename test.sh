#!/bin/bash

if [ "$1" == "-v" ]; then
	VERBOSE='true'
fi
if [ "$1" == "-vv" ]; then
	VERBOSE='very'
fi

pkill go-pluginserver
rq --help >/dev/null

echo "pwd: $PWD"

SOCKET='go_pluginserver.sock'

if pgrep go-pluginserver -l; then
	PREVIOUS_SERVER="yes"
else
	echo "starting server..."
	[ -S "$SOCKET" ] && rm "$SOCKET"
	./go-pluginserver -kong-prefix . &
	pgrep go-pluginserver -l
	sleep 0.1s
fi

msg() {
	query="$1"
	[ -v VERBOSE ] && rq <<< "$query"
	[ "$VERBOSE" == "very" ] && rq <<< "$query" -M | hd
	METHOD="$(rq <<< "$query" -- 'at([2])')"
	response="$(rq <<< "$query" -M | ncat -U "$SOCKET" | rq -m | jq 'select(.[0]==1)' )"
	[ -v VERBOSE ] && rq <<< "$response"

	ERROR="$(jq <<< "$response" '.[2]')"
	RESULT="$(jq <<< "$response" '.[3]')"
#	echo $RESULT
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

	fld_v="$(query_result '.'$fld'')"
	if [[ "$fld_v" =~ "$pattern" ]]; then
		echo "==> $fld_v : ok"
	else
		echo "==> $fld_v : no match '$pattern'"
		exit 1
	fi
}

query_result() {
	jq <<< "$RESULT" "$1"
}

msg '[0, 19, "plugin.SetPluginDir", ["'$PWD'"]]'
assert_noerr

msg '[0, 19, "plugin.GetStatus", []]'
assert_noerr
assert_fld_match 'Plugins' '{}'

msg '[0, 19, "plugin.GetPluginInfo", ["go-hello"]]'
assert_noerr

msg '[0, 19, "plugin.StartInstance", [{"Name":"go-hello", "Config":"{\"message\":\"howdy\"}"}]]'
assert_noerr
helloId=$(query_result '."Id"')
echo "helloId: $helloId"

msg '[0, 19, "plugin.StartInstance", [{"Name":"go-log", "Config":"{\"reopen\":false, \"path\":\"/some/where/else/\"}"}]]'
assert_noerr
logId=$(query_result '."Id"')
echo "logId: $logId"

msg '[0, 19, "plugin.HandleEvent", [{"InstanceId": '$helloId', "EventName": "access", "Params": [45, 23]}]]'
assert_noerr
helloEventId=$(query_result '."EventId"')

assert_fld_match 'Data.Method' 'kong.request.get_header'
assert_fld_match 'Data.Args' '"host"'

msg '[0, 20, "plugin.HandleEvent", [{"InstanceId": '$logId', "EventName": "log", "Params": [45, 23]}]]'
assert_noerr
logEventId=$(query_result '."EventId"')

assert_noerr
assert_fld_match 'Data.Method' 'kong.log.serialize'

# msg '[0, 19, "plugin.StepError", [{"EventId": '$helloEventId', "Data": "not in the mood for routes"}]]'
#msg "$(cat <<-EOF
#[
#  0, 19, "plugin.StepRoute",
#  [{
#    "Data": {
#      "created_at": 1574445198,
#      "https_redirect_status_code": 426,
#      "id": "c0ba987b-99e0-4342-a255-61ff47d54fe6",
#      "paths": [ "/" ],
#      "preserve_host": false,
#      "protocols": [ "http", "https" ],
#      "regex_priority": 0,
#      "service": {
#        "id": "a1a72823-4c75-42b3-92e6-79c865175287"
#      },
#      "strip_path": true,
#      "updated_at": 1574445198
#    },
#    "EventId": 0
#  }]
#]
#EOF
#)"
#assert_noerr
#
#callBack=$(query_result '."Data"')
#
#assert_fld_match 'Data' '"ret"'


msg '[0, 19, "plugin.InstanceStatus", ['$helloId']]'
assert_noerr

msg "[0, 19, \"plugin.CloseInstance\", [$helloId]]"
assert_noerr

msg '[0, 19, "plugin.InstanceStatus", ['$logId']]'
assert_noerr

msg "[0, 19, \"plugin.CloseInstance\", [$logId]]"
assert_noerr

msg '[0, 19, "plugin.GetStatus", []]'
assert_noerr
assert_fld_match 'Plugins["go-hello"]' '"Name": "go-hello"'
assert_fld_match 'Plugins["go-log"]' '"Name": "go-log"'


if [ ! -v PREVIOUS_SERVER ]; then
	pkill go-pluginserver
	rm "$SOCKET"
fi

