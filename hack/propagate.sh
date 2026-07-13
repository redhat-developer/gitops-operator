#!/bin/bash

set -e

FROM_BRANCH="master"
SKIP_MAKE=false

usage() {
  echo "Usage: $0 [--from-branch <branch>] [--skip-make]"
  echo ""
  echo "Options:"
  echo "  --from-branch <branch>  argocd-operator repo branch or tag to source manifests from (default: master)"
  echo "  --skip-make             skip running 'make bundle' after copying changed files (default: false)"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --from-branch)
      FROM_BRANCH="$2"
      shift 2
      ;;
    --skip-make)
      SKIP_MAKE=true
      shift
      ;;
    *)
      usage
      ;;
  esac
done

trap cleanup EXIT

function cleanup {
  echo "Removing /tmp/argocd-operator-hack"
  rm  -rf /tmp/argocd-operator-hack
}

mkdir -p /tmp/argocd-operator-hack

git clone --depth 1 --branch "$FROM_BRANCH" --single-branch --no-tags https://github.com/argoproj-labs/argocd-operator.git /tmp/argocd-operator-hack/

changedFiles=$(diff -qr /tmp/argocd-operator-hack/config/crd/bases/  ../config/crd/bases/ | grep -v argoproj.io_argocdexports.yaml | grep differ | awk -F ' ' '{print $2}')

echo "Changed Files"
echo $changedFiles

if [ -z "$changedFiles" ]
then
      echo "No difference found"
else
      cp ${changedFiles} ../config/crd/bases/
      if [ "$SKIP_MAKE" = false ]; then
        cd ..
        make bundle
      else
        echo "Skipping 'make bundle' (--skip-make specified)"
      fi
fi
