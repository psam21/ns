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
    # First encrypt the message using NIP-44 v2
    ENCRYPTED_MESSAGE=$(nak encrypt --recipient-pubkey "$recipient_pubkey" --sec "$sender_privkey" "$message")
    # Create unsigned chat event (kind 14)
    echo -e "${YELLOW}Sender: Creating encrypted chat event...${NC}"
    CHAT_EVENT=$(nak event -k 14 -c "$ENCRYPTED_MESSAGE" --sec "$sender_privkey" -p "$recipient_pubkey")
    # Seal the chat message (kind 13)
    echo -e "${YELLOW}Sender: Sealing message...${NC}"
    SEALED_EVENT=$(nak event -k 13 -c "$CHAT_EVENT" --sec "$sender_privkey" --created-at $(get_random_timestamp))
    # Gift-wrap the sealed message (kind 1059) - encrypt the seal with NIP-44
    echo -e "${YELLOW}Sender: Gift-wrapping message...${NC}"
    # First encrypt the sealed event using NIP-44
    ENCRYPTED_SEAL=$(nak encrypt --recipient-pubkey "$recipient_pubkey" --sec "$sender_privkey" "$SEALED_EVENT")
    # Then create the gift wrap event with encrypted content
    GIFT_WRAPPED=$(nak event -k 1059 -c "$ENCRYPTED_SEAL" --sec "$sender_privkey" -p "$recipient_pubkey" --created-at $(get_random_timestamp))
    # Extract the event ID for later use
    EVENT_ID=$(echo "$GIFT_WRAPPED" | jq -r '.id')
    
    # Publish to relay
    echo -e "${YELLOW}Sender: Publishing gift-wrapped message...${NC}"
    echo "$GIFT_WRAPPED" | nak --sec "$sender_privkey" --auth event "$RELAY"
    
    # Wait a moment for the event to be processed
    sleep 2
    
    # Verify the event was stored
    echo -e "${YELLOW}Sender: Verifying event was stored...${NC}"
    RESPONSE=$(nak --sec "$sender_privkey" --auth req -i "$EVENT_ID" "$RELAY")
    echo -e "${GREEN}Sender: Message sent successfully with ID: $EVENT_ID${NC}"
    
    # Export the EVENT_ID for use in the receiver function
    export EVENT_ID
}

fetch_published_event() {
    local event_id=$1
    local attempts=${NIP17_FETCH_ATTEMPTS:-6}
    local response
    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if response=$(nak --sec "$RECIPIENT_PRIVKEY" --auth req -i "$event_id" "$RELAY" 2>/dev/null) && \
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
    
    # Extract and decrypt the gift-wrapped content
    GIFT_WRAPPED_CONTENT=$(echo "$RECIPIENT_SUB" | jq -r '.content')
    echo -e "${YELLOW}Receiver: Decrypting gift-wrapped content...${NC}"
    
    # Decrypt the gift-wrapped content to get the sealed event
    SEALED_EVENT=$(nak decrypt --sec "$recipient_privkey" -p "$sender_pubkey" "$GIFT_WRAPPED_CONTENT")
    if [ $? -ne 0 ]; then
        echo -e "${RED}Error: Failed to decrypt gift-wrapped content${NC}"
        echo -e "${RED}Content was: $GIFT_WRAPPED_CONTENT${NC}"
        return 1
    fi
    # Parse the chat event
    CHAT_EVENT=$(echo "$SEALED_EVENT" | jq -r '.content')
    if [ $? -ne 0 ]; then
        echo -e "${RED}Error: Failed to parse chat event${NC}"
        echo -e "${RED}Content was: $SEALED_EVENT${NC}"
        return 1
    fi
    # Extract recipient tags from chat event
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
        
        # Extract the encrypted content from the chat event
        ENCRYPTED_MESSAGE=$(echo "$CHAT_EVENT" | jq -r '.content')
        if [ $? -ne 0 ]; then
            echo -e "${RED}Error: Failed to extract encrypted content${NC}"
            echo -e "${RED}Content was: $CHAT_EVENT${NC}"
            return 1
        fi
        # Decrypt the message
        echo -e "${YELLOW}Receiver: Attempting to decrypt message...${NC}"
        DECRYPTED_MESSAGE=$(nak decrypt --sec "$recipient_privkey" -p "$sender_pubkey" "$ENCRYPTED_MESSAGE")
        if [ $? -ne 0 ]; then
            echo -e "${RED}Error: Failed to decrypt message${NC}"
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