#!/bin/bash

function showHelp() {\
    echo "This script gets the latest release build failures for metal-ipi jobs"
    echo "(only if there isn't any newer passing build for that job)"
    echo 
    echo "Usage: metal-ipi-releases [-h|-c] <ver>"
    echo "Options:"
    echo "-h    Show this help"
    echo "-c    Use the locally cached results, skip downloading Prow results"
    echo "<ver> Filter by version, e.g. 4.9 (optional)"
    exit 1 
}

if [ "$1" = "-h" ]; then
  showHelp
  exit 1
fi

# Download the current Prow status
if [ "$1" = "-c" ]; then 
    ver=$2
else
    ver=$1
    echo "Fetching results from Prow, please wait"
    curl -s https://deck-ci.apps.ci.l2s4.p1.openshiftapps.com/\data.js > .prow-jobs.json
fi

# Filter results by job name
filter="periodic-ci-openshift-release-master-nightly-$ver.*metal-ipi.*"
jobs=$(jq --arg nf $filter -r '[ .[] | select(.job|test($nf)) | select(.type=="periodic")]' .prow-jobs.json)

# For every distinct job, get the latest build in case it failed
entry=""
for k in $(echo $jobs | jq -r '[ .[].job ] | unique | .[]'); do
    entry=${entry}$(echo $jobs | jq --arg job "$k" -r '[ .[] | select(.job|test($job)) | select(.state=="failure")] | sort_by(.job) | max_by(.finished) | select(. != null)')
done 

if [ -z "$entry" ]; then
    echo "No failures found!"
else
    echo "Failures:"
    echo $entry | jq -r '([.job, .url]) | @tsv' | column -t
fi
