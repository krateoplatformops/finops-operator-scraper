#!/bin/bash

if [ -z "${CONTAINER_TOOL}" ]; then
    CONTAINER_TOOL=docker
else
    CONTAINER_TOOL=${CONTAINER_TOOL}
fi

$(echo $CONTAINER_TOOL) buildx build --tag ${IMG} --push --platform linux/amd64,linux/arm64 .


