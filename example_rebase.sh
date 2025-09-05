#!/bin/bash

# Example usage of rebase_automation.sh
# This demonstrates how to rebase cert-manager-operator to a new version

# Set the versions you want to upgrade to
export NEW_CERT_MANAGER_VERSION="1.18.2"  # Change this to your target cert-manager version
export NEW_BUNDLE_VERSION="1.18.0"        # Change this to your target bundle version

# Optional: Set old versions explicitly (otherwise they'll be auto-detected)
# export OLD_BUNDLE_VERSION="1.18.0"
# export OLD_CERT_MANAGER_VERSION="1.18.2"

echo "=== Cert-Manager Operator Rebase Example ==="
echo "Target cert-manager version: $NEW_CERT_MANAGER_VERSION"
echo "Target bundle version: $NEW_BUNDLE_VERSION"
echo ""

# First, do a dry run to see what would be changed
echo "=== Step 1: Dry Run ==="
./rebase_automation.sh --dry-run

echo ""
read -p "Do you want to proceed with the actual rebase? (y/N): " -n 1 -r
echo

if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "=== Step 2: Running Actual Rebase ==="
    ./rebase_automation.sh
    
    echo ""
    echo "=== Rebase Complete! ==="
    echo "Please review the changes and test before pushing:"
    echo "  git log --oneline -4"
    echo "  git diff HEAD~4"
    echo ""
    echo "If everything looks good:"
    echo "  git push origin $(git branch --show-current)"
else
    echo "Rebase cancelled."
fi 