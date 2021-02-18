#!/bin/bash

# Download the release binary
export VERSION="v0.17.0"

# Set platform information
export ARCH=$(uname -m)
export OS=$(uname | awk '{print tolower($0)}')

# Download v0.17.0 binary for your platform
export OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/${VERSION}
curl -LO ${OPERATOR_SDK_DL_URL}/operator-sdk-${VERSION}-${ARCH}-${OS}-gnu

# Install the release binary in your CI PATH
chmod +x operator-sdk-${VERSION}-${ARCH}-${OS}-gnu && mv operator-sdk-${VERSION}-${ARCH}-${OS}-gnu operator-sdk

# Assert operator-sdk installation
operator-sdk version
