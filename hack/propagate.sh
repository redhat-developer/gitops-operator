#!/bin/bash

set -e

trap cleanup EXIT

function cleanup {
  echo "Removing /tmp/argocd-operator-hack"
  rm  -rf /tmp/argocd-operator-hack
}

mkdir -p /tmp/argocd-operator-hack

git clone https://github.com/argoproj-labs/argocd-operator.git /tmp/argocd-operator-hack/

changedFiles=$(diff -qr /tmp/argocd-operator-hack/config/crd/bases/  ../config/crd/bases/ | grep -v argoproj.io_argocdexports.yaml | grep differ | awk -F ' ' '{print $2}')

echo "Changed Files"
echo $changedFiles

if [ -z "$changedFiles" ]
then
      echo "No difference found"
else
      cp ${changedFiles} ../config/crd/bases/ 
      cd ..
      make bundle
fi
