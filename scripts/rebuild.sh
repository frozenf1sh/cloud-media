#!/bin/bash

docker build --target worker -t cloud-media-worker .
docker build --target api-server -t cloud-media-api-server .
docker compose up -d --build
