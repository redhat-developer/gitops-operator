#!/bin/bash

go mod tidy
PKGS=`go list  ./... | grep -v test/e2e`
go test -mod=readonly $PKGS
