#!/usr/bin/env bash

oldauth=$(mktemp)
newauth=$(mktemp)

# Get current information
oc get secrets pull-secret -n openshift-config -o template='{{index .data ".dockerconfigjson"}}' | base64 -d > ${oldauth}

# Get Brew registry credentials
brew_secret=$(jq '.auths."brew.registry.redhat.io".auth' ${HOME}/.docker/config.json | tr -d '"')

# Append the key:value to the JSON file
jq --arg secret ${brew_secret} '.auths |= . + {"brew.registry.redhat.io":{"auth":$secret}}' ${oldauth} > ${newauth}

# Update the pull-secret information in OCP
oc set data secret pull-secret -n openshift-config --from-file=.dockerconfigjson=${newauth}

# Cleanup
rm -f ${oldauth} ${newauth}
