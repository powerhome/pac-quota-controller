#!/bin/bash
set -x

curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
chmod +x ./kind
mv ./kind /usr/local/bin/kind

curl -L -o kubebuilder.tar.gz https://go.kubebuilder.io/dl/latest/linux/amd64
tar -xzf kubebuilder.tar.gz
mv kubebuilder/bin/kubebuilder /usr/local/bin/
rm -rf kubebuilder kubebuilder.tar.gz

KUBECTL_VERSION=$(curl -L -s https://dl.k8s.io/release/stable.txt)
curl -LO "https://dl.k8s.io/release/$KUBECTL_VERSION/bin/linux/amd64/kubectl"
chmod +x kubectl
mv kubectl /usr/local/bin/kubectl

docker network create -d=bridge --subnet=172.19.0.0/24 kind

kind version
kubebuilder version
docker --version
go version
kubectl version --client
