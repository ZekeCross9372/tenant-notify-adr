#!/usr/bin/env sh
# Exercise delivery for an already-onboarded tenant without provisioning resources.
set -eu

: "${INFRAI_API_KEY:?set INFRAI_API_KEY first}"
TENANT="${1:-acme}"
OWNER="${2:-u1}"

go build -o notifyd .

./notifyd session  -tenant "$TENANT" -member "$OWNER" -admin
./notifyd emit     -tenant "$TENANT" -state active -kind billing.invoice_failed -seq 7 \
                   -title "Card declined for the July invoice"
./notifyd presence -tenant "$TENANT"
