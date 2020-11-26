#!/bin/sh

# change to script's directory to make paths relative to it
cd $(dirname "$0")

# cleanup old things
rm -fr temp bin

set -e

ROOT=../..

echo "Copying the code..."
mkdir -p temp bin
cp $ROOT/go.mod $ROOT/go.sum temp
cp -rv $ROOT/manager temp
cp -rv $ROOT/agent temp
cp -r  $ROOT/vendor temp
cp static-build.sh temp

echo "Compiling... (IF HUNG, KILL THE CONTAINER!)"
docker run --rm -v $(pwd)/temp:/home -v $(pwd)/bin:/home/bin \
    ghcr.io/linksmart/deployer/build:golang-linux-amd64-stretch sh static-build.sh

echo "Cleaning up..."
rm -fr temp

echo "Success!"

