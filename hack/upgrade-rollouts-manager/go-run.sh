#!/bin/bash

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

cd $SCRIPT_DIR

# Read Github Token and Username from settings.env, if it exists
vars_file="$SCRIPT_DIR/settings.env"
if [[ -f "$vars_file" ]]; then
    source "$vars_file"
fi

# Run the upgrade code
go run .
