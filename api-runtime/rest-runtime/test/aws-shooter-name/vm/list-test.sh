#!/bin/bash
source ../setup.env

for NAME in "${CONNECT_NAMES[@]}"
do
	echo ========================== $NAME
	curl -sX GET http://$RESTSERVER:1024/spider/vm -H 'Content-Type: application/json' -d '{ "ConnectionName": "'${NAME}'" }' |json_pp &
done
