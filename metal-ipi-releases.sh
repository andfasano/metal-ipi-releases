#!/bin/bash

function showHelp() {
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

CACHE_FOLDER=.releases
mkdir -p $CACHE_FOLDER

function fetchReleasesConfig() {
    MAJOR_VERSION=4
    BASE_MINOR_VERSION=6
    releases_url="https://raw.githubusercontent.com/openshift/release/master/core-services/release-controller/_releases/"

    echo "Fetching release jobs configurations"

    for (( i=$BASE_MINOR_VERSION; ;i++)); do
        file="release-ocp-$MAJOR_VERSION.$i.json"
        url=$releases_url$file
        if ! curl -o .releases/$file --silent --fail "$url"; then
            break
        fi
    done
}

function checkForRefresh() {
    # Download the current Prow status
    if [ "$1" = "-c" ]; then 
        ver=$2
    else
        ver=$1
        echo "Fetching latest job results from Prow, please wait"
        curl -s https://deck-ci.apps.ci.l2s4.p1.openshiftapps.com/\data.js > .prow-jobs.json
        fetchReleasesConfig
    fi
}

function getJobNames() {
    for config in "$CACHE_FOLDER/*"; do
        metalBlocking=${metalBlocking}$(jq -r '.verify | with_entries(select((.key|test("metal-ipi")) and (.value.optional == null or .value.optional == false))) | .[] | .prowJob.name' $config)
        metalInforming=${metalInforming}$(jq -r '.verify | with_entries(select((.key|test("metal-ipi")) and (.value.optional == true) and (.value.upgrade == null or .value.upgrade == false))) | .[] | .prowJob.name' $config)
        metalUpgrades=${metalUpgrades}$(jq -r '.verify | with_entries(select((.key|test("metal-ipi")) and (.value.optional == true) and (.value.upgrade == true))) | .[] | .prowJob.name' $config)
    done
}

checkForRefresh $@
getJobNames

# Prefilter metal jobs by name/version
filter="periodic-ci-openshift-release-master-nightly-$ver.*metal-ipi.*"
allCurrentMetalPeriodics=$(jq --arg nf $filter -r '[ .[] | select(.job|test($nf)) | select((.type=="periodic") and (.state!="pending"))]' .prow-jobs.json)

function showResultsFor () {

    local jobs
    for job in $1; do
        jobs=${jobs}$(echo $allCurrentMetalPeriodics | jq --arg nf $job -r '[ .[] | select(.job == $nf)]')
    done

    # For every distinct job, get the latest build in case it failed
    entry=""
    for k in $(echo $jobs | jq -r '[ .[].job ] | unique | .[]'); do
        entry=${entry}$(echo $jobs | jq --arg job "$k" -r '[ .[] | select(.job==$job)] | sort_by(.job) | max_by(.started) | select((. != null) and (.state=="failure"))')
    done 

    if [ ! -z "$entry" ]; then
        echo
        echo "$2 failures:"
        echo "------------------------------"
        echo $entry | jq -r '([.job, (.started|tonumber|todateiso8601), .url]) | @tsv' | column -t
    fi
}

showResultsFor "$metalInforming" "Informing"
showResultsFor "$metalUpgrades" "Informing (Upgrade)"
showResultsFor "$metalBlocking" "Blocking"
