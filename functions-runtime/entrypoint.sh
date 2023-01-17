#!/bin/sh

set -eu

deno run --allow-net --allow-env=DATA,SECURE_DATA,CONTEXT,NODE_CLUSTER_SCHED_POLICY,NODE_UNIQUE_ID "$SCRIPT"
