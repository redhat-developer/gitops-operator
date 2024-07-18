#!/bin/bash

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

cd $SCRIPT_DIR

# Read Github Token and Username from settings.env, if it exists
vars_file="$SCRIPT_DIR/settings.env"
if [[ -f "$vars_file" ]]; then
    source "$vars_file"
fi

# Clone fork of gitops-operator repo

rm -rf "$SCRIPT_DIR/gitops-operator" || true

git clone "git@github.com:$GITHUB_FORK_USERNAME/gitops-operator"
cd gitops-operator

# Add a remote back to the original repo

git remote add parent "git@github.com:redhat-developer/gitops-operator"
git fetch parent

