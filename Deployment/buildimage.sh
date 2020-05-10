#!/bin/sh -e

SCRIPT_DIR=$(cd $(dirname $0); pwd)

cd $SCRIPT_DIR/../AllocateService/mod_allocator-service/
docker build -t localimage/mod_allocator-service:0.1 .

cd $SCRIPT_DIR/../GameServer/mod_simple-udp/
docker build -t localimage/mod_simple-udp:0.1 .

cd $SCRIPT_DIR/../OpenMatch/mod_matchmaker101/frontend/
docker build -t localimage/mod_frontend:0.1 .

cd $SCRIPT_DIR/../OpenMatch/mod_matchmaker101/matchfunction/
docker build -t localimage/mod_matchfunction:0.1 .

cd $SCRIPT_DIR/../OpenMatch/mod_matchmaker101/director/
docker build -t localimage/mod_director:0.1 .