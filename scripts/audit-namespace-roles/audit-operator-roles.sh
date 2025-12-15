#!/bin/bash

# ---------------------------------------------------------
# Pre-flight Check: Verify jq is installed
# ---------------------------------------------------------
if ! command -v jq &> /dev/null; then
    printf "Error: 'jq' is not installed.\n"
    printf "This script requires jq to parse Kubernetes JSON output.\n"
    exit 1
fi

# ---------------------------------------------------------
# CONFIGURATION
# ---------------------------------------------------------
TARGET_API="argoproj.io"
TARGET_RESOURCE="applications" 
TARGET_LABEL_KEY="app.kubernetes.io/part-of"
TARGET_LABEL_VAL="argocd"

printf "=========================================================\n"
printf "SEARCH CRITERIA (Must match ALL):\n"
printf "  1. API/Resource: %s / %s\n" "$TARGET_API" "$TARGET_RESOURCE"
printf "  2. Label:        %s=%s\n" "$TARGET_LABEL_KEY" "$TARGET_LABEL_VAL"
printf "  3. Scope:        Cross-namespace only\n"
printf "=========================================================\n"

printf "\nScanning Cluster (this may take a moment)...\n"

# ---------------------------------------------------------
# STEP 1: FIND CANDIDATE ROLES
# ---------------------------------------------------------
CANDIDATE_ROLES_JSON=$(oc get roles -A -o json -l "${TARGET_LABEL_KEY}=${TARGET_LABEL_VAL}" | jq -r --arg API "$TARGET_API" \
                                                    --arg RES "$TARGET_RESOURCE" \
                                                    --arg L_KEY "$TARGET_LABEL_KEY" \
                                                    --arg L_VAL "$TARGET_LABEL_VAL" '
  [
    .items[] |
    select(
      (.metadata.labels?[$L_KEY] == $L_VAL)
      and
      (
        .rules[]? | 
        ( (.apiGroups[]? == $API) or (.apiGroups[]? == "*") ) and 
        ( (.resources[]? == $RES) or (.resources[]? == "*") )
      )
    ) |
    "\(.metadata.namespace)/\(.metadata.name)"
  ] | unique
')

# If no candidate roles exist, we can exit early
if [ "$CANDIDATE_ROLES_JSON" == "[]" ]; then
    printf "  • No Roles found matching label/rule criteria.\n"
    exit 0
fi

# ---------------------------------------------------------
# FIND BINDINGS
# ---------------------------------------------------------
# We process ALL bindings, but filter down to only those that:
#   a) Point to a "Candidate Role" found in Step 1
#   b) Have at least one Subject in a DIFFERENT namespace
# We save this filtered JSON array to a variable.
TARGET_BINDINGS_JSON=$(oc get rolebindings -A -o json -l "${TARGET_LABEL_KEY}=${TARGET_LABEL_VAL}" | jq --argjson TARGET_ROLES "$CANDIDATE_ROLES_JSON" '
  [
    .items[] |
    (.metadata.namespace + "/" + .roleRef.name) as $localRef |
    .metadata.namespace as $binding_ns |

    # Filter A: Must reference one of our Candidate Roles
    select(
       .roleRef.kind == "Role" and 
       ($localRef as $ref | $TARGET_ROLES | index($ref))
    ) |

    # Filter B: Must have at least one cross-namespace ServiceAccount
    select(
      [ 
        .subjects[]? | 
        select(.kind == "ServiceAccount" and .namespace != $binding_ns) 
      ] | length > 0
    )
  ]
')

# ---------------------------------------------------------
# OUTPUT ROLES
# ---------------------------------------------------------
printf "\nRoles with cross-namespace access:\n"

# We extract the unique list of roles strictly from the OFFENDING bindings.
VERIFIED_ROLES=$(echo "$TARGET_BINDINGS_JSON" | jq -r '
  [ .[] | "\(.metadata.namespace)/\(.roleRef.name)" ] | unique
')

if [ "$VERIFIED_ROLES" == "[]" ]; then
    printf "  • No cross-namespace bindings found for the candidate roles.\n"
    printf "Scan Complete.\n"
    exit 0
else
    echo "$VERIFIED_ROLES" | jq -r '.[] | "  • Role: \((.))"'
fi

# ---------------------------------------------------------
# OUTPUT BINDINGS
# ---------------------------------------------------------
printf "\nCross-namespace bindings detail:\n"

echo "$TARGET_BINDINGS_JSON" | jq -r '
  .[] |
  .metadata.namespace as $binding_ns |

  # Calculate aggregate list of external namespaces for summary
  (
    [ 
      .subjects[]? | 
      select(.kind == "ServiceAccount" and .namespace != $binding_ns) | 
      .namespace 
    ] 
    | unique 
    | join(", ")
  ) as $external_namespaces |

  "--------------------------------------------------",
  "BINDING:   \(.metadata.namespace) / \(.metadata.name)",
  "ROLE REF:  \(.roleRef.name)",
  "SUBJECTS (cross-namespace only):",
  (
    .subjects[]? | 
    # Print only external service accounts
    if (.kind == "ServiceAccount" and .namespace != $binding_ns) then
      "  • \(.kind): \(.name) (ns: \(.namespace))"
    else
      empty
    end
  ),
  "",
  "• Namespace \($external_namespaces) has access to \(.metadata.namespace)",
  ""
'
