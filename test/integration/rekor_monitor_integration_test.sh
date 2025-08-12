#!/usr/bin/env bash
#
# Copyright 2024 The Sigstore Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -ex

pushd $HOME

echo "downloading service repos"
for repo in rekor ; do
    if [[ ! -d $repo ]]; then
        git clone https://github.com/sigstore/${repo}.git
    else
        pushd $repo
        git pull
        popd
    fi
done

docker_compose="docker compose"

echo "starting services"
for repo in rekor ; do
    pushd $repo
    ${docker_compose} up -d
    echo -n "waiting up to 60 sec for system to start"
    count=0
    until [ $(${docker_compose} ps | grep -c "(healthy)") == 5 ];
    do
        if [ $count -eq 6 ]; then
           echo "! timeout reached"
           exit 1
        else
           echo -n "."
           sleep 10
           let 'count+=1'
        fi
    done
    popd
done

function cleanup_services() {
    echo "cleaning up"
    for repo in rekor; do
        pushd $HOME/$repo
        ${docker_compose} down
        popd
    done
}
trap cleanup_services EXIT

# Install Rekor CLI
if ! command -v rekor-cli &>/dev/null; then
    echo "Installing Rekor CLI..."
    go install github.com/sigstore/rekor/cmd/rekor-cli@latest
    export PATH="$PATH:$(go env GOPATH)/bin"
fi

echo "Preparing artifact and signing..."
echo "hello rekor" > artifact.txt
openssl genrsa -out private.key 2048
openssl rsa -in private.key -pubout > public.key
openssl dgst -sha256 -sign private.key -out artifact.sig artifact.txt

echo "Uploading test entry to Rekor..."
rekor-cli upload \
  --artifact artifact.txt \
  --signature artifact.sig \
  --public-key public.key \
  --pki-format x509 \
  --type rekord \
  --rekor_server http://localhost:3000

sha=$(sha256sum artifact.txt | cut -d ' ' -f1)
rekor-cli search --sha "$sha" --rekor_server http://localhost:3000

echo
echo "running tests"
popd
go test -v -timeout=1m ./test/integration/...
