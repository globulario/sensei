#!/usr/bin/env bash

# Returns the owning repo for a generated output directory.
# Output:
#   awareness-graph  generated artifact is owned by this repo and blocks --check
#   services         generated artifact is owned by the paired services repo
#   external         anything else / unknown
generated_output_owner() {
    local output_dir="$1" ag_generated="$2" svc_generated="$3"
    if [[ "$output_dir" == "$ag_generated" ]]; then
        printf 'awareness-graph\n'
        return 0
    fi
    if [[ "$output_dir" == "$svc_generated" ]]; then
        printf 'services\n'
        return 0
    fi
    printf 'external\n'
}

generated_output_blocks_check() {
    [[ "$(generated_output_owner "$1" "$2" "$3")" == "awareness-graph" ]]
}
