#!/bin/bash
# Recording cleanup script - to be run via cron
# This script deletes recordings older than DAYS_TO_KEEP

set -e

# Default to 30 days if not specified
DAYS_TO_KEEP=${1:-30}

# Directory containing recordings (relative to project root)
RECORDING_DIR="recordings"

# Get the project root directory (parent of scripts directory)
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( dirname "$SCRIPT_DIR" )"
FULL_RECORDING_DIR="$PROJECT_ROOT/$RECORDING_DIR"

# Check if recording directory exists
if [ ! -d "$FULL_RECORDING_DIR" ]; then
    echo "Error: Recording directory $FULL_RECORDING_DIR does not exist."
    exit 1
fi

echo "Starting cleanup of recordings older than $DAYS_TO_KEEP days..."
echo "Recording directory: $FULL_RECORDING_DIR"

# Find files older than DAYS_TO_KEEP days and delete them
FILES_DELETED=$(find "$FULL_RECORDING_DIR" -type f -mtime +$DAYS_TO_KEEP -name "*.wav" -delete -print | wc -l)

echo "Cleanup complete. Deleted $FILES_DELETED files."

# Export disk usage statistics
DISK_USAGE=$(du -sh "$FULL_RECORDING_DIR" | awk '{print $1}')
DISK_FREE=$(df -h "$FULL_RECORDING_DIR" | awk 'NR==2 {print $4}')
DISK_USED_PCT=$(df "$FULL_RECORDING_DIR" | awk 'NR==2 {print $5}')

echo "Current disk usage:"
echo "  Recording space used: $DISK_USAGE"
echo "  Free space: $DISK_FREE"
echo "  Disk usage: $DISK_USED_PCT"

exit 0