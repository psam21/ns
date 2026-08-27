#!/bin/bash

# NIP-59: Gift Wrap
# Tests gift wrapping functionality with kinds 13 (seal) and 1059 (gift wrap)
# https://github.com/nostr-protocol/nips/blob/master/59.md

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'

# Test counters
total_tests=0
successful_tests=0
failed_tests=0

# Relay URL - use local relay for testing
RELAY="${RELAY:-${RELAY_URL:-ws://localhost:8080}}"
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=/dev/null
source "$SCRIPT_DIR/tools/nip42_fetch.sh"

# Function to check dependencies
check_dependencies() {
    for cmd in nak; do
        if ! command -v $cmd &> /dev/null; then
            echo -e "${RED}Error: $cmd is not installed${NC}"
            echo "Install with: go install github.com/fiatjaf/nak@latest"
            exit 1
        fi
    done
}

# Check dependencies
check_dependencies

# Function to check relay connectivity over the same authenticated WebSocket path
check_relay() {
    local auth_key=$1
    echo "Checking relay connection..."
    # The probe has its own bounded context; invoke the sourced shell function directly.
    if ! authenticated_check "$auth_key" >/dev/null 2>&1; then
        echo -e "${RED}Relay not accessible at $RELAY${NC}" >&2
        echo "Start the relay or set RELAY_URL to an authorized test endpoint." >&2
        exit 1
    fi
    echo -e "${GREEN}Relay is accessible${NC}"
}

# Helper function to print test results
print_result() {
    local test_name=$1
    local success=$2
    local message=$3
    
    total_tests=$((total_tests + 1))
    if [ "$success" = true ]; then
        successful_tests=$((successful_tests + 1))
        echo -e "${GREEN}✓ Test $total_tests: $test_name - $message${NC}"
    else
        failed_tests=$((failed_tests + 1))
        echo -e "${RED}✗ Test $total_tests: $test_name - $message${NC}"
    fi
}

# Function to get random timestamp within last 2 days
get_random_timestamp() {
    local now=$(date +%s)
    local two_days_ago=$((now - 172800)) # 2 days in seconds
    local random_offset=$((RANDOM % 172800))
    echo $((two_days_ago + random_offset))
}

echo -e "${BLUE}Testing NIP-59: Gift Wrap${NC}"
echo "=================================="

# Generate test keys
echo -e "${YELLOW}Generating test keys...${NC}"
SENDER_PRIVKEY=$(nak key generate)
SENDER_PUBKEY=$(nak key public $SENDER_PRIVKEY)
RECIPIENT_PRIVKEY=$(nak key generate)
RECIPIENT_PUBKEY=$(nak key public "$RECIPIENT_PRIVKEY")

# Authenticate before exercising private gift-wrap reads.
check_relay "$SENDER_PRIVKEY"

echo "Sender public key: $SENDER_PUBKEY"
echo "Recipient public key: $RECIPIENT_PUBKEY"

# Test 1: Create a standards-compliant gift wrap event (kind 1059)
echo -e "\n${YELLOW}Test 1: Creating a basic gift wrap event (kind 1059)${NC}"
TEST_MESSAGE="Hello from NIP-59 gift wrap!"
SIGNED_RUMOR=$(nak event -k 1 -c "$TEST_MESSAGE" --sec "$SENDER_PRIVKEY" -p "$RECIPIENT_PUBKEY")
RUMOR_EVENT=$(echo "$SIGNED_RUMOR" | jq -c 'del(.sig)')
SEALED_EVENT=$(nak event -k 13 -c "$(nak encrypt --recipient-pubkey "$RECIPIENT_PUBKEY" --sec "$SENDER_PRIVKEY" "$RUMOR_EVENT")" --sec "$SENDER_PRIVKEY" --created-at "$(get_random_timestamp)")
WRAPPER_PRIVKEY=$(nak key generate)
WRAPPER_PUBKEY=$(nak key public "$WRAPPER_PRIVKEY")
ENCRYPTED_MESSAGE=$(nak encrypt --recipient-pubkey "$RECIPIENT_PUBKEY" --sec "$WRAPPER_PRIVKEY" "$SEALED_EVENT")
GIFT_WRAP_EVENT=$(nak event -k 1059 -c "$ENCRYPTED_MESSAGE" --sec "$WRAPPER_PRIVKEY" -p "$RECIPIENT_PUBKEY" --created-at "$(get_random_timestamp)")
GIFT_WRAP_ID=$(echo "$GIFT_WRAP_EVENT" | jq -er '.id')
response=$(authenticated_publish "$SENDER_PRIVKEY" "$GIFT_WRAP_EVENT" 2>&1)
echo "Response: $response"
if [[ "$response" == *"accepted for event $GIFT_WRAP_ID"* ]]; then
    print_result "Basic gift wrap event" true "Event accepted by relay"
    echo "Gift wrap event ID: $GIFT_WRAP_ID"
else
    print_result "Basic gift wrap event" false "Failed to create event: $response"
fi

# Test 2: Create a gift wrap with multiple recipients
echo -e "\n${YELLOW}Test 2: Creating a gift wrap with multiple recipients${NC}"
RECIPIENT2_PRIVKEY=$(nak key generate)
RECIPIENT2_PUBKEY=$(nak key public $RECIPIENT2_PRIVKEY)

MULTI_RECIPIENT_WRAP=$(nak event -k 1059 -c "$ENCRYPTED_MESSAGE" --sec "$WRAPPER_PRIVKEY" -p "$RECIPIENT_PUBKEY" -p "$RECIPIENT2_PUBKEY" --created-at "$(get_random_timestamp)")
if response=$(authenticated_publish "$SENDER_PRIVKEY" "$MULTI_RECIPIENT_WRAP" 2>&1); then
    print_result "Gift wrap with multiple recipients" true "Event accepted by relay"
else
    print_result "Gift wrap with multiple recipients" false "Relay rejected event: $response"
fi

# Test 3: Test gift wrap without 'p' tag (should fail or be accepted based on relay policy)
echo -e "\n${YELLOW}Test 3: Creating a gift wrap without 'p' tag${NC}"
NO_RECIPIENT_WRAP=$(nak event -k 1059 -c "$ENCRYPTED_MESSAGE" --sec "$WRAPPER_PRIVKEY" --created-at "$(get_random_timestamp)")
if response=$(authenticated_publish "$SENDER_PRIVKEY" "$NO_RECIPIENT_WRAP" 2>&1); then
    print_result "Gift wrap without p tag" true "Event accepted (relay policy allows)"
else
    print_result "Gift wrap without p tag" true "Event rejected (relay enforces p tag requirement)"
fi

# Test 4: Test gift wrap with invalid NIP-44 content (should fail)
echo -e "\n${YELLOW}Test 4: Creating a gift wrap with invalid NIP-44 content${NC}"
INVALID_CONTENT="plain text not encrypted"
INVALID_WRAP=$(nak event -k 1059 -c "$INVALID_CONTENT" --sec "$WRAPPER_PRIVKEY" -p "$RECIPIENT_PUBKEY" --created-at "$(get_random_timestamp)")
if response=$(authenticated_publish "$SENDER_PRIVKEY" "$INVALID_WRAP" 2>&1); then
    print_result "Gift wrap with invalid content" false "Event should have been rejected but was accepted"
else
    print_result "Gift wrap with invalid content" true "Event correctly rejected for invalid NIP-44 content"
fi

# Test 5: Test gift wrap with empty content (should fail)
echo -e "\n${YELLOW}Test 5: Creating a gift wrap with empty content${NC}"
EMPTY_WRAP=$(nak event -k 1059 -c "" --sec "$WRAPPER_PRIVKEY" -p "$RECIPIENT_PUBKEY" --created-at "$(get_random_timestamp)")
if response=$(authenticated_publish "$SENDER_PRIVKEY" "$EMPTY_WRAP" 2>&1); then
    print_result "Gift wrap with empty content" false "Event should have been rejected but was accepted"
else
    print_result "Gift wrap with empty content" true "Event correctly rejected for empty content"
fi

# Test 6: Retrieve the recipient-only gift wrap by exact ID.
echo -e "\n${YELLOW}Test 6: Retrieving recipient gift wrap by exact ID${NC}"
if [ -n "${GIFT_WRAP_ID:-}" ]; then
    sleep 2
    search_response=$(authenticated_fetch "$RECIPIENT_PRIVKEY" "$GIFT_WRAP_ID")
    if printf '%s' "$search_response" | jq -e --arg id "$GIFT_WRAP_ID" --arg recipient "$RECIPIENT_PUBKEY" \
        'type == "object" and .id == $id and .kind == 1059 and any(.tags[]?; .[0] == "p" and .[1] == $recipient)' >/dev/null; then
        print_result "Retrieve recipient gift wrap" true "Exact recipient-authorized event returned"
    else
        print_result "Retrieve recipient gift wrap" false "Exact event or recipient tag was not returned"
    fi
else
    print_result "Retrieve recipient gift wrap" false "No gift-wrap ID was produced"
fi

# Test 7: Verify the outer wrapper is decryptable with the recipient key.
if [ -n "${GIFT_WRAP_ID:-}" ]; then
    echo -e "\n${YELLOW}Test 7: Decrypting the kind-1059 outer wrapper${NC}"
    specific_response=$(authenticated_fetch "$RECIPIENT_PRIVKEY" "$GIFT_WRAP_ID")
    WRAPPER_PUBKEY=$(printf '%s' "$specific_response" | jq -er '.pubkey')
    OUTER_CONTENT=$(printf '%s' "$specific_response" | jq -er '.content')
    if SEALED_EVENT=$(nak decrypt --sec "$RECIPIENT_PRIVKEY" -p "$WRAPPER_PUBKEY" "$OUTER_CONTENT" 2>/dev/null) && \
        printf '%s' "$SEALED_EVENT" | jq -e --arg sender "$SENDER_PUBKEY" '.kind == 13 and .pubkey == $sender and (.tags | length) == 0' >/dev/null; then
        print_result "Decrypt outer gift wrap" true "Recovered valid sender-signed seal"
    else
        print_result "Decrypt outer gift wrap" false "Could not recover a valid kind-13 seal"
    fi
else
    print_result "Decrypt outer gift wrap" false "No gift-wrap ID was produced"
fi

# Test 8: Decrypt the seal and verify the plaintext rumor.
if [ -n "${GIFT_WRAP_ID:-}" ]; then
    echo -e "\n${YELLOW}Test 8: Testing gift wrap end-to-end decryption${NC}"
    if [ -z "${SEALED_EVENT:-}" ]; then
        specific_response=$(authenticated_fetch "$RECIPIENT_PRIVKEY" "$GIFT_WRAP_ID")
        WRAPPER_PUBKEY=$(printf '%s' "$specific_response" | jq -er '.pubkey')
        OUTER_CONTENT=$(printf '%s' "$specific_response" | jq -er '.content')
        SEALED_EVENT=$(nak decrypt --sec "$RECIPIENT_PRIVKEY" -p "$WRAPPER_PUBKEY" "$OUTER_CONTENT" 2>/dev/null) || true
    fi
    SEALED_CONTENT=$(printf '%s' "${SEALED_EVENT:-}" | jq -er '.content' 2>/dev/null || true)
    if [ -n "$SEALED_CONTENT" ] && RUMOR_EVENT=$(nak decrypt --sec "$RECIPIENT_PRIVKEY" -p "$SENDER_PUBKEY" "$SEALED_CONTENT" 2>/dev/null) && \
        printf '%s' "$RUMOR_EVENT" | jq -e --arg recipient "$RECIPIENT_PUBKEY" --arg message "$TEST_MESSAGE" \
            '.kind == 1 and .content == $message and any(.tags[]?; .[0] == "p" and .[1] == $recipient) and (.sig == null)' >/dev/null; then
        print_result "Gift wrap end-to-end decryption" true "Recovered plaintext unsigned rumor"
    else
        print_result "Gift wrap end-to-end decryption" false "Failed to recover the expected rumor"
    fi
else
    print_result "Gift wrap end-to-end decryption" false "No gift-wrap ID was produced"
fi

# Test 9: Test with wallet connect content (kind 13194 inside gift wrap)
echo -e "\n${YELLOW}Test 9: Creating gift wrap containing wallet connect content${NC}"
WALLET_CONTENT='{"uri":"wc:test123","name":"Test Wallet"}'
WALLET_SIGNED_RUMOR=$(nak event -k 13194 -c "$WALLET_CONTENT" --sec "$SENDER_PRIVKEY" -p "$RECIPIENT_PUBKEY")
WALLET_RUMOR=$(echo "$WALLET_SIGNED_RUMOR" | jq -c 'del(.sig)')
WALLET_SEAL=$(nak event -k 13 -c "$(nak encrypt --recipient-pubkey "$RECIPIENT_PUBKEY" --sec "$SENDER_PRIVKEY" "$WALLET_RUMOR")" --sec "$SENDER_PRIVKEY" --created-at "$(get_random_timestamp)")
WALLET_WRAPPER_PRIVKEY=$(nak key generate)
WALLET_WRAP=$(nak event -k 1059 -c "$(nak encrypt --recipient-pubkey "$RECIPIENT_PUBKEY" --sec "$WALLET_WRAPPER_PRIVKEY" "$WALLET_SEAL")" --sec "$WALLET_WRAPPER_PRIVKEY" -p "$RECIPIENT_PUBKEY" --created-at "$(get_random_timestamp)")
if response=$(authenticated_publish "$SENDER_PRIVKEY" "$WALLET_WRAP" 2>&1); then
    print_result "Gift wrap with wallet connect content" true "Event accepted by relay"
else
    print_result "Gift wrap with wallet connect content" false "Relay rejected event: $response"
fi

# Print summary
echo -e "\n=================================="
echo -e "${BLUE}NIP-59 Test Summary:${NC}"
echo "Total Tests: $total_tests"
echo -e "${GREEN}Successful Tests: $successful_tests${NC}"
echo -e "${RED}Failed Tests: $failed_tests${NC}"

if [ $failed_tests -gt 0 ]; then
    echo -e "\n${RED}Some tests failed. Check relay logs for details.${NC}"
    exit 1
else
    echo -e "\n${GREEN}All tests passed! NIP-59 Gift Wrap functionality is working correctly.${NC}"
fi