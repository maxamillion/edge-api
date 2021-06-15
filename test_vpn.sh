#!/bin/bash

if ! /usr/bin/curl http://file.rdu.redhat.com/ &> /dev/null; then
    printf "ERROR: Must be on the VPN\n"
fi

curl -X POST -H "Content-Type: application/json" -d '{"Name": "test000", "ImageBuildHash": "b990a2d2-abf3-44f0-be49-731d43cfab92", "ParentHash": "", "BuildDate": "20210608", "BuildNumber": 1, "ImageBuildTarURL": "http://file.rdu.redhat.com/~admiller/rhel_edge_ostree_tars/0.0.0-b990a2d2-abf3-44f0-be49-731d43cfab92-commit.tar", "OSTreeCommit": "ae65525d9b7d8d343d39aad5458b52951e1def73c05c0cb5a625f3b641efe98e", "Arch": "x86_64" }' localhost:3000/api/edge/v1/commits/

curl -X POST -H "Content-Type: application/json" -d '{"Name": "test001", "ImageBuildHash": "802f86fe-812f-4dc3-80d6-17cdd8ffd654", "ParentHash": "", "BuildDate": "20210609", "BuildNumber": 1, "ImageBuildTarURL": "http://file.rdu.redhat.com/~admiller/rhel_edge_ostree_tars/0.0.1-802f86fe-812f-4dc3-80d6-17cdd8ffd654-commit.tar", "OSTreeCommit": "c510383b369d0ecfd39022a303a32ef0516b5971b9a9e428fdee4281d3c122e7", "Arch": "x86_64" }' localhost:3000/api/edge/v1/commits/

curl -X POST -H "Content-Type: application/json" -d '{"UpdateCommitID": 1, "InventoryHosts": "foobar"}' localhost:3000/api/edge/v1/commits/updates/


cat <<EOF 
Here we're going to demo the edge-api REST API interface
We'll walk through a few different things: commits, updates, and repos

First, commits: OSTree Commit tarballs that we'll get from Hosted Image Builder
Second, updates: OSTree repo compose of the desired update commit, 
    a set of Inventory hosts, all old OSTreee commits discovered from Inventory Hosts
    metadata, and then a static delta generation between the versions. This is then
    stored back into S3 as an OSTree Repo that's ready to serve.
Third, repos: a Proxy of S3 content that handles identity management and tenancy
    for cloud.redhat.com accounts.
EOF

# curl localhost:3000/api/edge/v1/commits/1 | jq
# curl -X POST -H "Content-Type: application/json" -d '{"UpdateCommitID": 1, "InventoryHosts": "foobar"}' localhost:3000/api/edge/v1/commits/updates/
# curl localhost:3000/api/edge/v1/commits/updates/20 | jq
# 
