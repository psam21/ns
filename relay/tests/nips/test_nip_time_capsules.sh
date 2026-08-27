#!/bin/bash

# NIP-XX Time Capsules Test Script - Updated Specification
# Tests time-lock encrypted messages with new format and NIP-59 support

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

RELAY="${RELAY:-${RELAY_URL:-ws://localhost:8080}}"

# Function to check relay connectivity
check_relay() {
    echo "🔗 Checking relay connection at $RELAY..."
    if ! timeout 10 nak relay info "$RELAY" >/dev/null 2>&1; then
        echo -e "${RED}❌ Relay not accessible at $RELAY${NC}"
        echo "Start the relay or set RELAY_URL to an authorized test endpoint."
        return 1
    fi
    
    echo -e "${GREEN}✅ Relay is accessible${NC}"
    return 0
}

# Main test execution
main() {
    echo "🕐 Testing NIP-XX Time Capsules Implementation"
    echo "=============================================="
    
    # Check relay
    if ! check_relay; then
        exit 1
    fi
    
    echo ""
    echo "Running NIP-XX Time Capsules Python Test..."
    echo "=============================================="
    
    # Run the comprehensive Python test
    if python3 tests/nips/nip-xx-time-capsules/lib/test_nip_time_capsules.py; then
        echo ""
        echo "=============================================="
        echo -e "${GREEN}🎉 NIP-XX Time Capsules test completed successfully!${NC}"
        exit 0
    else
        echo ""
        echo "=============================================="
        echo -e "${RED}❌ NIP-XX Time Capsules test failed!${NC}"
        exit 1
    fi
}

# Run main function
main "$@"
