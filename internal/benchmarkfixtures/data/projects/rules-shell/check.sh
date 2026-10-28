#!/bin/sh
set -eu
awk -F= '{ values[$1] = $2 } END { printf "{\"file\":\"policy/discount.ivo-fixture\",\"rate_percent\":%d,\"threshold\":%d,\"result\":%d}\n", values["rate_percent"], values["threshold"], 100 - values["rate_percent"] }' policy/discount.ivo-fixture
