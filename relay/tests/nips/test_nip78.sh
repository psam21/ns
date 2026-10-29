#!/bin/bash

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color
BLUE='\033[0;34m'
YELLOW='\033[1;33m'

# Test counter
test_count=0
success_count=0
fail_count=0

# Relay URL
# RELAY="ws://localhost:8080"
RELAY="${RELAY:-${RELAY_URL:-ws://localhost:8080}}"

# Helper function to print test results
print_result() {
    local test_name=$1
    local success=$2
    local nip=$3
    
    if [ "$success" = true ]; then
        echo -e "${GREEN}✓ Test $test_count: $test_name (NIP-$nip)${NC}"
        ((success_count++))
    else
        echo -e "${RED}✗ Test $test_count: $test_name (NIP-$nip)${NC}"
        ((fail_count++))
    fi
    ((test_count++))
}

echo -e "${BLUE}Starting nostr.ltd relay NIP-78 Tests${NC}\n"

# Test NIP-78: Application-specific Data
echo -e "\n${YELLOW}Testing NIP-78: Application-specific Data${NC}"

# NIP-78 requires a d tag for addressable kind:30078 events. Content and all
# other tags are application-defined and may contain encrypted/non-JSON data.
OPAQUE_DATA=$(nak event -k 30078 -c 'AgFzdG9yZWQtY2lwaGVydGV4dA==' -t d=test-app-opaque $RELAY 2>&1)
if [ $? -eq 0 ] && [[ "$OPAQUE_DATA" != *"NIP validation failed"* ]]; then
    print_result "Accept opaque application-specific content without p tag" true "78"
else
    print_result "Accept opaque application-specific content without p tag" false "78"
fi

JSON_DATA=$(nak event -k 30078 -c '{"private":true,"setting":"dark"}' -t d=test-app-json -t custom=application-defined $RELAY 2>&1)
if [ $? -eq 0 ] && [[ "$JSON_DATA" != *"NIP validation failed"* ]]; then
    print_result "Accept JSON content and arbitrary application tags" true "78"
else
    print_result "Accept JSON content and arbitrary application tags" false "78"
fi

MISSING_D=$(nak event -k 30078 -c 'opaque content without an identifier' $RELAY 2>&1)
if [ $? -ne 0 ] || [[ "$MISSING_D" == *"must have 'd' tag"* ]]; then
    print_result "Reject application-specific data without a d tag" true "78"
else
    print_result "Reject application-specific data without a d tag" false "78"
fi

# Print summary
echo -e "\n${BLUE}Test Summary:${NC}"
echo -e "Total tests: $test_count"
echo -e "${GREEN}Successful: $success_count${NC}"
echo -e "${RED}Failed: $fail_count${NC}"

# Exit with error if any tests failed
if [ $fail_count -gt 0 ]; then
    exit 1
else
    exit 0
fi 