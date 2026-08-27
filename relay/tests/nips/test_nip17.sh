#!/usr/bin/env bash

set -Eeuo pipefail

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color
BLUE='\033[0;34m'
YELLOW='\033[1;33m'

# Relay URL - should be from kind 10050 list
RELAY="${RELAY:-${RELAY_URL:-ws://localhost:8080}}"
# RELAY="wss://relay.example.com"
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=/dev/null
source "$SCRIPT_DIR/tools/nip42_fetch.sh"

# Function to check if nak is installed
check_nak() {
    if ! command -v nak &> /dev/null; then
        echo -e "${RED}Error: nak (Nostr Army Knife) is not installed${NC}"
        echo "Please install nak first: https://github.com/fiatjaf/nak"
        exit 1
    fi
}

# Function to generate test keys
generate_test_keys() {
    echo -e "${YELLOW}Generating test keys...${NC}"
    
    # Generate sender keys
    SENDER_PRIVKEY=$(nak key generate)
    SENDER_PUBKEY=$(nak key public "$SENDER_PRIVKEY")
    
    # Generate recipient keys
    RECIPIENT_PRIVKEY=$(nak key generate)
    RECIPIENT_PUBKEY=$(nak key public "$RECIPIENT_PRIVKEY")
    
    echo -e "${GREEN}Generated ephemeral test keys without printing secret material.${NC}"
}

# Function to get random timestamp within last 2 days
get_random_timestamp() {
    local now=$(date +%s)
    local two_days_ago=$((now - 172800)) # 2 days in seconds
    local random_offset=$((RANDOM % 172800))
    echo $((two_days_ago + random_offset))
}

# Function to simulate sender
simulate_sender() {
    local message=$1
    local recipient_pubkey=$2
    local sender_privkey=$3
    
    echo -e "${YELLOW}Sender: Encrypting message...${NC}"
    # Create a plaintext kind-14 rumor, then remove its signature before sealing.
    echo -e "${YELLOW}Sender: Creating plaintext kind-14 rumor...${NC}"
    SIGNED_RUMOR=$(nak event -k 14 -c "$message" --sec "$sender_privkey" -p "$recipient_pubkey")
    RUMOR_EVENT=$(echo "$SIGNED_RUMOR" | jq -c 'del(.sig)')

    # Seal the unsigned rumor with the sender's real key.
    echo -e "${YELLOW}Sender: Sealing message...${NC}"
    SEALED_EVENT=$(nak event -k 13 -c "$(nak encrypt --recipient-pubkey "$recipient_pubkey" --sec "$sender_privkey" "$RUMOR_EVENT")" --sec "$sender_privkey" --created-at "$(get_random_timestamp)")

    # Gift-wrap the seal with a fresh one-time wrapper key, as required by NIP-59.
    echo -e "${YELLOW}Sender: Gift-wrapping message...${NC}"
    WRAPPER_PRIVKEY=$(nak key generate)
    WRAPPER_PUBKEY=$(nak key public "$WRAPPER_PRIVKEY")
    ENCRYPTED_SEAL=$(nak encrypt --recipient-pubkey "$recipient_pubkey" --sec "$WRAPPER_PRIVKEY" "$SEALED_EVENT")
    GIFT_WRAPPED=$(nak event -k 1059 -c "$ENCRYPTED_SEAL" --sec "$WRAPPER_PRIVKEY" -p "$recipient_pubkey" --created-at "$(get_random_timestamp)")
    EVENT_ID=$(echo "$GIFT_WRAPPED" | jq -er '.id')

    # Publish and await the relay's OK on the same authenticated connection.
    echo -e "${YELLOW}Sender: Publishing gift-wrapped message...${NC}"
    authenticated_publish "$sender_privkey" "$GIFT_WRAPPED"
    echo -e "${GREEN}Sender: Message sent successfully with ID: $EVENT_ID${NC}"
    
    # Export the EVENT_ID for use in the receiver function
    export EVENT_ID
}

fetch_published_event() {
    local event_id=$1
    local attempts=${NIP17_FETCH_ATTEMPTS:-6}
    local response
    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if response=$(authenticated_fetch "$RECIPIENT_PRIVKEY" "$event_id" 2>/dev/null) && \
            printf '%s' "$response" | jq -e --arg id "$event_id" 'select(type == "object" and .id == $id)' >/dev/null 2>&1; then
            printf '%s' "$response"
            return 0
        fi
        sleep "${NIP17_FETCH_DELAY:-2}"
    done
    echo -e "${RED}Error: Event $event_id was not retrievable after $attempts attempts${NC}" >&2
    return 1
}

# Function to simulate receiver
simulate_receiver() {
    local recipient_privkey=$1
    local sender_pubkey=$2
    local recipient_pubkey=$3
    local message=${NIP17_MESSAGE:-automated NIP-17 integration test message}
    local event_id=$EVENT_ID
    
    echo -e "${YELLOW}Receiver: Checking for message with ID: $event_id${NC}"
    echo -e "${YELLOW}Receiver: Fetching specific event by ID${NC}"
    # Fetch the exact event after asynchronous storage has had time to commit.
    RECIPIENT_SUB=$(fetch_published_event "$event_id") || return 1

    # First, check if we got a valid JSON response
    if ! echo "$RECIPIENT_SUB" | jq -e --arg id "$event_id" 'type == "object" and .id == $id' >/dev/null 2>&1; then
        echo -e "${RED}Error: Failed to get the published gift-wrap event${NC}" >&2
        return 1
    fi

    # Extract all p tags from gift-wrapped event and check if any match the recipient pubkey
    RECIPIENT_TAGS=$(echo "$RECIPIENT_SUB" | jq -r '.tags[] | select(.[0] == "p") | .[1]')
    echo -e "${YELLOW}Receiver: Found recipient tags in gift-wrapped event: $RECIPIENT_TAGS${NC}"
    
    # Decrypt the outer wrapper with the recipient key and one-time wrapper pubkey.
    GIFT_WRAPPED_CONTENT=$(echo "$RECIPIENT_SUB" | jq -er '.content')
    WRAPPER_PUBKEY=$(echo "$RECIPIENT_SUB" | jq -er '.pubkey')
    echo -e "${YELLOW}Receiver: Decrypting gift-wrapped content...${NC}"
    SEALED_EVENT=$(nak decrypt --sec "$recipient_privkey" -p "$WRAPPER_PUBKEY" "$GIFT_WRAPPED_CONTENT") || {
        echo -e "${RED}Error: Failed to decrypt gift-wrapped content${NC}" >&2
        return 1
    }
    SEALED_PUBKEY=$(echo "$SEALED_EVENT" | jq -er '.pubkey')
    SEALED_KIND=$(echo "$SEALED_EVENT" | jq -er '.kind')
    SEALED_TAG_COUNT=$(echo "$SEALED_EVENT" | jq -er '.tags | length')
    if [[ "$SEALED_PUBKEY" != "$sender_pubkey" || "$SEALED_KIND" != "13" || "$SEALED_TAG_COUNT" != "0" ]]; then
        echo -e "${RED}Error: Invalid NIP-59 seal structure${NC}" >&2
        return 1
    fi

    # Decrypt the seal to recover the unsigned plaintext kind-14 rumor.
    SEALED_CONTENT=$(echo "$SEALED_EVENT" | jq -er '.content')
    CHAT_EVENT=$(nak decrypt --sec "$recipient_privkey" -p "$sender_pubkey" "$SEALED_CONTENT") || {
        echo -e "${RED}Error: Failed to decrypt sealed rumor${NC}" >&2
        return 1
    }
    CHAT_RECIPIENT_TAGS=$(echo "$CHAT_EVENT" | jq -r '.tags[] | select(.[0] == "p") | .[1]')
    echo -e "${YELLOW}Receiver: Found recipient tags in chat event: $CHAT_RECIPIENT_TAGS${NC}"
    
    # Initialize found flag
    FOUND_MATCH=0
    
    # Debug: Show what we're comparing
    echo -e "${YELLOW}Receiver: Comparing tags with expected pubkey...${NC}"
    
    # Check each tag from gift-wrapped event
    while IFS= read -r tag; do
        if [ ! -z "$tag" ]; then
            # Remove any whitespace and convert to lowercase for comparison
            CLEAN_TAG=$(echo "$tag" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')
            CLEAN_EXPECTED_PUBKEY=$(echo "$recipient_pubkey" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')
            echo -e "${YELLOW}Receiver: Comparing cleaned tag '$CLEAN_TAG' with cleaned pubkey '$CLEAN_EXPECTED_PUBKEY'${NC}"
            if [ "$CLEAN_TAG" = "$CLEAN_EXPECTED_PUBKEY" ]; then
                echo -e "${GREEN}Receiver: Found match in gift-wrapped event!${NC}"
                FOUND_MATCH=1
                break
            fi
        fi
    done <<< "$RECIPIENT_TAGS"
    
    # If not found in gift-wrapped event, check chat event tags
    if [ $FOUND_MATCH -eq 0 ]; then
        while IFS= read -r tag; do
            if [ ! -z "$tag" ]; then
                # Remove any whitespace and convert to lowercase for comparison
                CLEAN_TAG=$(echo "$tag" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')
                CLEAN_EXPECTED_PUBKEY=$(echo "$recipient_pubkey" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')
                echo -e "${YELLOW}Receiver: Comparing cleaned tag '$CLEAN_TAG' with cleaned pubkey '$CLEAN_EXPECTED_PUBKEY'${NC}"
                if [ "$CLEAN_TAG" = "$CLEAN_EXPECTED_PUBKEY" ]; then
                    echo -e "${GREEN}Receiver: Found match in chat event!${NC}"
                    FOUND_MATCH=1
                    break
                fi
            fi
        done <<< "$CHAT_RECIPIENT_TAGS"
    fi
    
    if [ $FOUND_MATCH -eq 1 ]; then
        echo -e "${GREEN}Receiver: Found message addressed to us!${NC}"
        
        CHAT_KIND=$(echo "$CHAT_EVENT" | jq -er '.kind')
        CHAT_CONTENT=$(echo "$CHAT_EVENT" | jq -er '.content')
        if [[ "$CHAT_KIND" != "14" || "$CHAT_CONTENT" != "$message" ]]; then
            echo -e "${RED}Error: Rumor kind/content mismatch${NC}" >&2
            return 1
        fi
        echo -e "${GREEN}Receiver: Successfully decrypted the automated test message.${NC}"
        return 0
    else
        echo -e "${YELLOW}Receiver: Event not addressed to recipient (expected $recipient_pubkey)${NC}"
        return 1
    fi
}

# Main script
echo -e "${BLUE}Starting NIP-17 Sender/Receiver Simulation${NC}\n"

# Check for nak
check_nak

# Generate test keys
generate_test_keys

# Use a deterministic non-interactive message; callers may override it safely.
MESSAGE="${NIP17_MESSAGE:-automated NIP-17 integration test message}"

# Simulate sender
echo -e "\n${BLUE}=== Sender Simulation ===${NC}"
simulate_sender "$MESSAGE" "$RECIPIENT_PUBKEY" "$SENDER_PRIVKEY"

# Simulate receiver
echo -e "\n${BLUE}=== Receiver Simulation ===${NC}"
simulate_receiver "$RECIPIENT_PRIVKEY" "$SENDER_PUBKEY" "$RECIPIENT_PUBKEY"

echo -e "\n${GREEN}Simulation complete!${NC}" 