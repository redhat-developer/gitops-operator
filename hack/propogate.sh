#!/bin/bash

set -e

trap cleanup EXIT

function cleanup {
  echo "Removing /tmp/argocd-operator-hack"
  rm  -rf /tmp/argocd-operator-hack
}

function installgit { 

 pkgname=git
 which $pkgname > /dev/null;isPackage=$?
 if [ $isPackage != 0 ];then
        echo "$pkgname not installed"
        sleep 1
        read -r -p "${1:-$pkgname will be installed. Are you sure? [y/N]} " response
        case "$response" in
            [yY][eE][sS]|[yY]) 
                sudo apt-get install $pkgname
                ;;
            *)
                false
                ;;
        esac

 else
        echo "$pkgname is installed"
        sleep 1
 fi

}


installgit


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
      go mod vendor
      make bundle
fi
