#!/bin/bash
#
# dump-vm-logs.sh
#
# Runs `aegis vm list`, then for each unique VM name runs `aegis vm logs <name>`
# and concatenates everything into a single timestamped file.
#
# Usage:
#   ./scripts/dump-vm-logs.sh
#   # or after chmod +x
#
set -euo pipefail

# Output file with timestamp for uniqueness
timestamp=$(date +%Y%m%d-%H%M%S)
outfile="aegis-vm-logs-${timestamp}.log"

echo "Collecting VM list and logs into ${outfile} ..."

# Capture the raw list output (including header) at the top of the file
{
    echo "=== aegis vm list (raw) ==="
    ./bin/aegis vm list 2>&1 || echo "(vm list command failed)"
    echo
} > "${outfile}"

# Extract unique VM names.
# The list output typically looks like:
#   Running VMs:
#     court-persona-ciso  type=... status=...
#   Lines with VM names start with two or more spaces.
# We take the first field on those lines and deduplicate.
vms=$(./bin/aegis vm list 2>/dev/null \
    | awk 'NF && $1 ~ /^[[:alnum:]_-]+$/ && $0 ~ /^  / {print $1}' \
    | sort -u)

if [[ -z "$vms" ]]; then
    echo "No VMs found or unable to parse vm list." | tee -a "${outfile}"
    exit 0
fi

echo "Found VMs:"
echo "$vms" | sed 's/^/  - /'
echo

for vm in $vms; do
    echo "=== Logs for VM: ${vm} ===" | tee -a "${outfile}"
    if ./bin/aegis vm logs "${vm}" >> "${outfile}" 2>&1; then
        :  # success
    else
        echo "(vm logs command returned non-zero for ${vm})" >> "${outfile}"
    fi
    echo >> "${outfile}"
done

echo "Done. All output concatenated in: ${outfile}"
echo "You can view it with: less ${outfile}  or  cat ${outfile}"
