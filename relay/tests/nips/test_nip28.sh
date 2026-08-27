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

# Helper function to check if event was accepted
check_event_accepted() {
    local response=$1
    # Check if the response contains an acceptance message or event ID
    if [[ "$response" == *"\"true\""* ]] || [[ "$response" == *"published"* ]] || [[ "$response" == *"id"* ]]; then
        return 0  # success
    else
        return 1  # failure
    fi
}

# Helper function to extract event ID from nak output
extract_event_id() {
    local output=$1
    # Try to extract event ID from various possible formats
    echo "$output" | grep -o '[a-f0-9]\{64\}' | head -1
}

echo -e "${BLUE}Starting nostr.ltd relay NIP-28 (Public Chat) Tests${NC}\n"
echo -e "${YELLOW}Testing NIP-28: Public Chat Events (Kinds 40-44)${NC}\n"

# Test 1: Create Channel (Kind 40)
echo -e "Test 1: Channel Creation (Kind 40)..."
CHANNEL_RESPONSE=$(nak event -k 40 --content '{"name": "Test Channel", "about": "A test channel for NIP-28 testing", "picture": "https://example.com/channel.jpg", "relays": ["wss://backup.example.com", "wss://relay.example.com"]}' $RELAY 2>&1)
CHANNEL_ID=$(extract_event_id "$CHANNEL_RESPONSE")

if [ ! -z "$CHANNEL_ID" ] && check_event_accepted "$CHANNEL_RESPONSE"; then
    print_result "Create channel with metadata" true "28"
    echo -e "   Channel ID: $CHANNEL_ID"
else
    print_result "Create channel with metadata" false "28"
    echo -e "   Response: $CHANNEL_RESPONSE"
fi

# Test 2: Channel Metadata Update (Kind 41)
if [ ! -z "$CHANNEL_ID" ]; then
    echo -e "\nTest 2: Channel Metadata Update (Kind 41)..."
    METADATA_RESPONSE=$(nak event -k 41 --content '{"name": "Updated Test Channel", "about": "Updated description for testing", "picture": "https://example.com/updated.jpg", "relays": ["wss://backup.example.com"]}' -t e="$CHANNEL_ID,wss://backup.example.com,root" -t t="testing" $RELAY 2>&1)
    
    if check_event_accepted "$METADATA_RESPONSE"; then
        print_result "Update channel metadata" true "28"
    else
        print_result "Update channel metadata" false "28"
        echo -e "   Response: $METADATA_RESPONSE"
    fi
fi

# Test 3: Channel Message (Kind 42)
if [ ! -z "$CHANNEL_ID" ]; then
    echo -e "\nTest 3: Channel Message (Kind 42)..."
    MESSAGE_RESPONSE=$(nak event -k 42 --content "Hello from the test channel! This is a root message. 👋" -t e="$CHANNEL_ID,wss://backup.example.com,root" $RELAY 2>&1)
    MESSAGE_ID=$(extract_event_id "$MESSAGE_RESPONSE")
    
    if [ ! -z "$MESSAGE_ID" ] && check_event_accepted "$MESSAGE_RESPONSE"; then
        print_result "Post channel message" true "28"
        echo -e "   Message ID: $MESSAGE_ID"
    else
        print_result "Post channel message" false "28"
        echo -e "   Response: $MESSAGE_RESPONSE"
    fi
fi

# Test 4: Reply Message (Kind 42 with reply structure)
if [ ! -z "$CHANNEL_ID" ] && [ ! -z "$MESSAGE_ID" ]; then
    echo -e "\nTest 4: Reply Message (Kind 42 with reply tags)..."
    # Get a dummy pubkey for testing (this would be the author of the message being replied to)
    DUMMY_PUBKEY="79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
    
    REPLY_RESPONSE=$(nak event -k 42 --content "This is a reply to the first message! 💬" \
        -t e="$CHANNEL_ID,wss://backup.example.com,root" \
        -t e="$MESSAGE_ID,wss://backup.example.com,reply" \
        -t p="$DUMMY_PUBKEY,wss://backup.example.com" \
        $RELAY 2>&1)
    
    if check_event_accepted "$REPLY_RESPONSE"; then
        print_result "Post reply message with proper threading" true "28"
    else
        print_result "Post reply message with proper threading" false "28"
        echo -e "   Response: $REPLY_RESPONSE"
    fi
fi

# Test 5: Hide Message (Kind 43)
if [ ! -z "$MESSAGE_ID" ]; then
    echo -e "\nTest 5: Hide Message (Kind 43)..."
    HIDE_RESPONSE=$(nak event -k 43 --content '{"reason": "Testing hide functionality"}' -t e="$MESSAGE_ID" $RELAY 2>&1)
    
    if check_event_accepted "$HIDE_RESPONSE"; then
        print_result "Hide message with reason" true "28"
    else
        print_result "Hide message with reason" false "28"
        echo -e "   Response: $HIDE_RESPONSE"
    fi
fi

# Test 6: Mute User (Kind 44)
echo -e "\nTest 6: Mute User (Kind 44)..."
MUTE_PUBKEY="79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
MUTE_RESPONSE=$(nak event -k 44 --content '{"reason": "Testing mute functionality"}' -t p="$MUTE_PUBKEY" $RELAY 2>&1)

if check_event_accepted "$MUTE_RESPONSE"; then
    print_result "Mute user with reason" true "28"
else
    print_result "Mute user with reason" false "28"
    echo -e "   Response: $MUTE_RESPONSE"
fi

echo -e "\n${YELLOW}Testing NIP-28 Validation Rules${NC}\n"

# Test 7: Invalid Channel Creation (no name)
echo -e "Test 7: Invalid Channel Creation (missing name)..."
INVALID_CHANNEL_RESPONSE=$(nak event -k 40 --content '{"about": "Channel without required name field"}' $RELAY 2>&1)

if [[ "$INVALID_CHANNEL_RESPONSE" == *"channel name is required"* ]] || [[ "$INVALID_CHANNEL_RESPONSE" == *"invalid"* ]]; then
    print_result "Reject channel creation without name" true "28"
else
    print_result "Reject channel creation without name" false "28"
    echo -e "   Response: $INVALID_CHANNEL_RESPONSE"
fi

# Test 8: Invalid Channel Message (missing e tag)
echo -e "\nTest 8: Invalid Channel Message (missing channel reference)..."
INVALID_MESSAGE_RESPONSE=$(nak event -k 42 --content "Message without channel reference" $RELAY 2>&1)

if [[ "$INVALID_MESSAGE_RESPONSE" == *"missing required 'e' tag"* ]] || [[ "$INVALID_MESSAGE_RESPONSE" == *"channel message must reference"* ]]; then
    print_result "Reject channel message without channel reference" true "28"
else
    print_result "Reject channel message without channel reference" false "28"
    echo -e "   Response: $INVALID_MESSAGE_RESPONSE"
fi

# Test 9: Invalid Hide Message (missing e tag)
echo -e "\nTest 9: Invalid Hide Message (missing message reference)..."
INVALID_HIDE_RESPONSE=$(nak event -k 43 --content '{"reason": "test"}' $RELAY 2>&1)

if [[ "$INVALID_HIDE_RESPONSE" == *"missing required 'e' tag"* ]] || [[ "$INVALID_HIDE_RESPONSE" == *"hide message event must reference"* ]]; then
    print_result "Reject hide message without message reference" true "28"
else
    print_result "Reject hide message without message reference" false "28"
    echo -e "   Response: $INVALID_HIDE_RESPONSE"
fi

# Test 10: Invalid Mute User (missing p tag)
echo -e "\nTest 10: Invalid Mute User (missing user reference)..."
INVALID_MUTE_RESPONSE=$(nak event -k 44 --content '{"reason": "test"}' $RELAY 2>&1)

if [[ "$INVALID_MUTE_RESPONSE" == *"missing required 'p' tag"* ]] || [[ "$INVALID_MUTE_RESPONSE" == *"mute user event must reference"* ]]; then
    print_result "Reject mute user without user reference" true "28"
else
    print_result "Reject mute user without user reference" false "28"
    echo -e "   Response: $INVALID_MUTE_RESPONSE"
fi

# Test 11: Invalid Channel Metadata (missing e tag)
echo -e "\nTest 11: Invalid Channel Metadata Update (missing channel reference)..."
INVALID_METADATA_RESPONSE=$(nak event -k 41 --content '{"name": "Updated"}' $RELAY 2>&1)

if [[ "$INVALID_METADATA_RESPONSE" == *"missing required 'e' tag"* ]] || [[ "$INVALID_METADATA_RESPONSE" == *"channel metadata update must reference"* ]]; then
    print_result "Reject metadata update without channel reference" true "28"
else
    print_result "Reject metadata update without channel reference" false "28"
    echo -e "   Response: $INVALID_METADATA_RESPONSE"
fi

# Test 12: Invalid Reply Structure (missing root tag)
if [ ! -z "$MESSAGE_ID" ]; then
    echo -e "\nTest 12: Invalid Reply Structure (missing root reference)..."
    INVALID_REPLY_RESPONSE=$(nak event -k 42 --content "Invalid reply without root" \
        -t e="$MESSAGE_ID,wss://backup.example.com,reply" \
        $RELAY 2>&1)
    
    if [[ "$INVALID_REPLY_RESPONSE" == *"reply must have"* ]] || [[ "$INVALID_REPLY_RESPONSE" == *"invalid reply structure"* ]]; then
        print_result "Reject invalid reply structure" true "28"
    else
        print_result "Reject invalid reply structure" false "28"
        echo -e "   Response: $INVALID_REPLY_RESPONSE"
    fi
fi

echo -e "\n${YELLOW}Testing NIP-28 JSON Validation${NC}\n"

# Test 13: Invalid JSON in channel metadata
echo -e "Test 13: Invalid JSON in channel metadata..."
INVALID_JSON_RESPONSE=$(nak event -k 40 --content '{"name": "Test", invalid json}' $RELAY 2>&1)

if [[ "$INVALID_JSON_RESPONSE" == *"invalid"* ]] || [[ "$INVALID_JSON_RESPONSE" == *"JSON"* ]]; then
    print_result "Reject invalid JSON in channel metadata" true "28"
else
    print_result "Reject invalid JSON in channel metadata" false "28"
    echo -e "   Response: $INVALID_JSON_RESPONSE"
fi

# Test 14: Invalid relay URLs in metadata
echo -e "\nTest 14: Invalid relay URLs in metadata..."
INVALID_RELAY_RESPONSE=$(nak event -k 40 --content '{"name": "Test", "relays": ["http://invalid.com", "ftp://bad.url"]}' $RELAY 2>&1)

if [[ "$INVALID_RELAY_RESPONSE" == *"invalid relay URL"* ]] || [[ "$INVALID_RELAY_RESPONSE" == *"invalid"* ]]; then
    print_result "Reject invalid relay URLs" true "28"
else
    print_result "Reject invalid relay URLs" false "28"
    echo -e "   Response: $INVALID_RELAY_RESPONSE"
fi

# Print summary
echo -e "\n${BLUE}======================================${NC}"
echo -e "${BLUE}NIP-28 Public Chat Test Summary${NC}"
echo -e "${BLUE}======================================${NC}"
echo -e "Total tests: $test_count"
echo -e "${GREEN}Successful: $success_count${NC}"
echo -e "${RED}Failed: $fail_count${NC}"

if [ $success_count -eq $test_count ]; then
    echo -e "${GREEN}🎉 All NIP-28 tests passed!${NC}"
elif [ $success_count -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Some NIP-28 tests failed.${NC}"
else
    echo -e "${RED}❌ All NIP-28 tests failed.${NC}"
fi

echo -e "\n${BLUE}NIP-28 Event Types Tested:${NC}"
echo -e "• Kind 40: Channel Create"
echo -e "• Kind 41: Channel Metadata Update"
echo -e "• Kind 42: Channel Message (root and reply)"
echo -e "• Kind 43: Hide Message"
echo -e "• Kind 44: Mute User"

echo -e "\n${BLUE}Validation Rules Tested:${NC}"
echo -e "• Required metadata fields"
echo -e "• Proper tag references (e, p tags)"
echo -e "• Reply structure validation"
echo -e "• JSON format validation"
echo -e "• Relay URL format validation"

# Exit with error if any tests failed
if [ $fail_count -gt 0 ]; then
    exit 1
else
    exit 0
fi 