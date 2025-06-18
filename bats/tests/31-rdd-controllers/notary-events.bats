load '../../helpers/load'

# Notary controller events tests - tests event generation and deduplication
# for the Notary controller including SpecUpdate, ValueRecorded, and NoChange events.
# For core controller functionality, see notary.bats

NOTARY_CONTROLLER_NAME="notary-controller"

local_setup_file() {
    setup_rdd_control_plane "notary"
}

create_notary() {
    local name=$1
    local value=$2
    local config_map_name=$3

    rdd ctl apply -f - <<EOF || return 1
apiVersion: rdd.rancherdesktop.io/v1alpha1
kind: Notary
metadata:
  name: ${name}
  namespace: default
spec:
  value: "${value}"
  configMapName: "${config_map_name}"
EOF
}

delete_notary() {
    local name=$1
    delete_resource "notary" "${name}"
}

update_notary_value() {
    local name=$1
    local value=$2
    patch_resource "notary" "${name}" "{\"spec\":{\"value\":\"${value}\"}}"
}

wait_for_notary_status() {
    local name=$1
    local expected=$2
    wait_for_resource_status "notary" "${name}" "lastRecordedValue" "${expected}"
}

assert_events_exist() {
    local resource_name=$1
    local reason=$2

    run -0 rdd ctl get events --field-selector involvedObject.name="${resource_name}" -o json
    run -0 jq ".items | map(select(.reason == \"$reason\")) | length" <<<"$output"
    refute_output 0
}

wait_for_events() {
    local resource_name=$1
    local reason=$2

    # couldn't figure out a way to use `wait` for events
    try --max 10 --delay 1 -- assert_events_exist "${resource_name}" "${reason}"
}

assert_events_after_timestamp() {
    local resource_name=$1
    local after_timestamp=$2
    local reason=$3

    run -0 rdd ctl get events  --field-selector involvedObject.name="${resource_name}" -o json
    run -0 jq ".items | map(select(.reason == \"$reason\" and .lastTimestamp > \"$after_timestamp\")) | length" <<<"$output"
    refute_output 0
}

count_events_with_reason() {
    local resource_name=$1
    local reason=$2

    run rdd ctl get events  --field-selector involvedObject.name="${resource_name}" -o json
    if [ "$status" -ne 0 ]; then
        echo "0"
        return
    fi

    jq ".items | map(select(.reason == \"$reason\")) | length" <<<"$output"
}

get_events_after_timestamp() {
    local resource_name=$1
    local after_timestamp=$2
    local reason=$3

    try --max 10 --delay 1 -- assert_events_after_timestamp "${resource_name}" "${after_timestamp}" "${reason}"

    run rdd ctl get events --field-selector involvedObject.name="${resource_name}" -o json
    if [ "$status" -ne 0 ]; then
        echo ""
        return 1
    fi

    jq -r ".items | map(select(.lastTimestamp > \"$after_timestamp\" and .reason == \"$reason\"))" <<<"$output"
}

get_latest_event_timestamp() {
    local resource_name=$1

    run rdd ctl get events  --field-selector involvedObject.name="${resource_name}" -o json
    if [ "$status" -ne 0 ]; then
        echo ""
        return 1
    fi

    jq -r ".items | sort_by(.lastTimestamp) | .[-1].lastTimestamp // empty" <<<"$output"
}

@test 'verify event generation for spec updates' {
    create_notary "events" "initial-event-value" "events-history"

    # Wait for initial ConfigMap creation and events
    wait_for_resource_count "configmaps" "$NOTARY_CONTROLLER_NAME" "events" 1
    wait_for_events "events" "SpecUpdate"
    wait_for_events "events" "ValueRecorded"

    # Check initial events - should have SpecUpdate and ValueRecorded events
    run -0 rdd ctl get events --field-selector involvedObject.name=events
    assert_output --partial "SpecUpdate"
    assert_output --partial "initial-event-value"
    assert_output --partial "ValueRecorded"

    # Get the timestamp of the most recent event before update
    run -0 get_latest_event_timestamp "events"
    local timestamp=$output

    # Update with a different value
    update_notary_value "events" "new-event-value"

    # Wait for status update and new events
    wait_for_notary_status "events" "new-event-value"

    # Verify we have new SpecUpdate and ValueRecorded events containing the new value
    run -0 get_events_after_timestamp "events" "$timestamp" "SpecUpdate"
    assert_output --partial "new-event-value"
    run -0 get_events_after_timestamp "events" "$timestamp" "ValueRecorded"
    assert_output --partial "new-event-value"
}

@test 'verify event generation when value unchanged' {
    create_notary "nochange" "unchanged-value" "nochange-history"

    # Wait for initial ConfigMap creation and events
    wait_for_resource_count "configmaps" "$NOTARY_CONTROLLER_NAME" "nochange" 1
    wait_for_events "nochange" "SpecUpdate"
    wait_for_events "nochange" "ValueRecorded"

    # Get the timestamp of the most recent event before update
    run -0 get_latest_event_timestamp "nochange"
    local timestamp=$output

    # First change to a different value to ensure we have a different state
    update_notary_value "nochange" "different-value"
    wait_for_notary_status "nochange" "different-value"

    # Now change back to the original value to test the "same value" scenario
    update_notary_value "nochange" "unchanged-value"
    wait_for_notary_status "nochange" "unchanged-value"

    # Check events - should have SpecUpdate and ValueRecorded events showing the changes
    run -0 rdd ctl get events --field-selector involvedObject.name=nochange
    assert_output --partial "SpecUpdate"
    assert_output --partial "unchanged-value"
    assert_output --partial "ValueRecorded"
}

@test 'test event deduplication behavior' {
    create_notary "dedup" "dedup-value" "dedup-history"

    # Wait for initial ConfigMap creation and events
    wait_for_resource_count "configmaps" "$NOTARY_CONTROLLER_NAME" "dedup" 1
    wait_for_events "dedup" "SpecUpdate"
    wait_for_events "dedup" "ValueRecorded"

    # Make multiple distinct changes to test event generation and deduplication
    update_notary_value "dedup" "value-1"
    wait_for_notary_status "dedup" "value-1"

    update_notary_value "dedup" "value-2"
    wait_for_notary_status "dedup" "value-2"

    update_notary_value "dedup" "value-3"
    wait_for_notary_status "dedup" "value-3"

    # Get all events for this resource
    run -0 rdd ctl get events --field-selector involvedObject.name=dedup -o wide

    # Count how many SpecUpdate and ValueRecorded events we have
    # Kubernetes should deduplicate similar events automatically
    local spec_update_count=$(count_events_with_reason "dedup" "SpecUpdate")
    local value_recorded_count=$(count_events_with_reason "dedup" "ValueRecorded")

    # We should have multiple events but they may be deduplicated by Kubernetes
    [ "$spec_update_count" -ge 1 ]
    [ "$value_recorded_count" -ge 1 ]

    # Verify we have events for multiple values
    assert_output --partial "value-1"
    assert_output --partial "value-2"
    assert_output --partial "value-3"
}

@test 'test no-change events with annotation updates' {
    create_notary "anno" "constant-value" "anno-history"

    # Wait for initial ConfigMap creation and events
    wait_for_resource_count "configmaps" "$NOTARY_CONTROLLER_NAME" "anno" 1
    wait_for_events "anno" "SpecUpdate"
    wait_for_events "anno" "ValueRecorded"

    # Get the timestamp of the most recent event before annotation update
    run -0 get_latest_event_timestamp "anno"
    local timestamp=$output

    # Update an annotation to force a reconcile without changing the value
    run -0 rdd ctl annotate notary anno test-annotation=test-value

    # Should have both SpecUpdate and NoChange events
    run -0 get_events_after_timestamp "anno" "$timestamp" "SpecUpdate"
    assert_output --partial "constant-value"

    run -0 get_events_after_timestamp "anno" "$timestamp" "NoChange"
    assert_output --partial "value unchanged"
}