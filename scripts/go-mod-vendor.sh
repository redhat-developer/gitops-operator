#!/usr/bin/env bash

go_mod_vendor_with_retry() {
    local retry status

    go mod vendor && return 0
    for retry in 1 2 3 4 5; do
        echo "go mod vendor failed. Retrying in 30 seconds (retry ${retry}/5)." >&2
        sleep 30 || return $?
        GODEBUG=http2client=0 go mod vendor && return 0
        status=$?
    done
    return "$status"
}
