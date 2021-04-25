#!/usr/bin/env bash
docker build --rm -t registry.hundsun.com/hcs/admission-webhook:v3 -f docker/Dockerfile .
