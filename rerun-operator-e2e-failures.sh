#!/bin/bash

if [ -z "$1" ]; then
  echo "Usage: $0 <log_file>"
  exit 1
fi

LOG_FILE=$1
if [ ! -f "$LOG_FILE" ]; then
  echo "File not found: $LOG_FILE"
  exit 1
fi

CLEAN_LOG=$(mktemp)
RERUN_LOG=$(mktemp)
CLEAN_RERUN=$(mktemp)
trap 'rm -f "$CLEAN_LOG" "$RERUN_LOG" "$CLEAN_RERUN"' EXIT

if ! command -v go &> /dev/null; then
  echo "Go is required."
  exit 1
fi

go mod tidy >/dev/null 2>&1
go mod download >/dev/null 2>&1
go mod vendor >/dev/null 2>&1

# Strip colors for processing
sed 's/\x1b\[[0-9;]*m//g' "$LOG_FILE" > "$CLEAN_LOG"

PARSE_AWK='
/\[FAIL\]/ {
  s="[Unknown]"
  if ($0 ~ /Sequential/) s="[Sequential]"
  if ($0 ~ /Parallel/) s="[Parallel]"
  match($0, /[0-9]-[0-9]{3}[-_][a-zA-Z0-9_-]+/);
  if (RLENGTH > 0) {
      val = substr($0, RSTART, RLENGTH);
      sub(/_test$/, "", val);
      print s " " val;
  }
}'

FAILURES=$(awk "$PARSE_AWK" "$CLEAN_LOG" | sort | uniq)
SUITE_FAILED=$(grep -c "A BeforeSuite node failed" "$CLEAN_LOG" || true)

if [ -z "$FAILURES" ] && [ "$SUITE_FAILED" -eq 0 ]; then
  echo "No test failures found."
  exit 0
fi

echo "Tests to rerun:"
[ -n "$FAILURES" ] && echo "$FAILURES" | sed 's/^/  /'
echo ""

GINKGO="./bin/ginkgo"
[ ! -x "$GINKGO" ] && GINKGO="ginkgo"

export OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-openshift-gitops-operator}"
SEQ_DIR="./test/openshift/e2e/ginkgo/sequential"
PAR_DIR="./test/openshift/e2e/ginkgo/parallel"

echo "⏳ Executing reruns in strict isolation..."
echo "----------------------------------------------------------------"

> "$RERUN_LOG"

if [ -n "$FAILURES" ]; then
  echo "$FAILURES" | while read -r line; do
    SUITE_TYPE=$(echo "$line" | awk '{print $1}')
    TEST_ID=$(echo "$line" | awk '{print $2}')
    
    if [ "$SUITE_TYPE" = "[Sequential]" ]; then
      SUITE_DIR="$SEQ_DIR"
    else
      SUITE_DIR="$PAR_DIR"
    fi
    
    echo -n "🏃 Running $TEST_ID... "
    
    TEST_OUTPUT=$(mktemp)
    "$GINKGO" -v -focus="$TEST_ID" -r "$SUITE_DIR" > "$TEST_OUTPUT" 2>&1
    
    sed 's/\x1b\[[0-9;]*m//g' "$TEST_OUTPUT" > "${TEST_OUTPUT}_clean"
    cat "${TEST_OUTPUT}_clean" >> "$CLEAN_RERUN"
    
    if grep -q "^FAIL!" "${TEST_OUTPUT}_clean"; then
      echo "❌ FAILED"
    elif grep -q "^SUCCESS!" "${TEST_OUTPUT}_clean" || grep -q "0 Failed" "${TEST_OUTPUT}_clean"; then
      TIME_TAKEN=$(grep -o "Ran [0-9]* of [0-9]* Specs in .*" "${TEST_OUTPUT}_clean" | sed 's/.* in //')
      if [ -z "$TIME_TAKEN" ]; then
        echo "⚠️ SKIPPED (Test did not execute)"
      else
        echo "✅ PASSED (Took: $TIME_TAKEN)"
      fi
    else
      echo "⚠️ UNKNOWN STATE (Check logs)"
    fi
    
    rm -f "$TEST_OUTPUT" "${TEST_OUTPUT}_clean"
  done
fi

STILL_FAILING=$(awk "$PARSE_AWK" "$CLEAN_RERUN" | sort | uniq)

echo "----------------------------------------------------------------"
echo "Rerun Detailed Error Logs:"
echo "----------------------------------------------------------------"

if [ -z "$STILL_FAILING" ]; then
  echo "All tests passed on rerun."
else
  echo "$STILL_FAILING" | while read -r line; do
    TEST_ID=$(echo "$line" | awk '{print $2}')
    echo "FAILED: $line"
    
    awk -v tid="$TEST_ID" '
      BEGIN { in_err=0; buf="" }
      /^[ \t]*•?[ \t]*\[(FAILED|PANICKED|FAIL)\]/ {
          in_err=1
          buf=$0
          next
      }
      in_err {
          buf = buf "\n" $0
          if ($0 ~ /^------------------------------/ || $0 ~ /^SSS/) {
              if (buf ~ tid) {
                  n = split(buf, lines, "\n")
                  valid_count = 0
                  
                  # First, collect all valid lines
                  for (i=1; i<=n; i++) {
                      line = lines[i]
                      sub(/^[ \t]+/, "", line)
                      if (line != "" && line !~ /^------------------------------/ && line !~ /^SSS/) {
                          valid_count++
                          cleaned[valid_count] = line
                      }
                  }
                  
                  # Print logic with truncation
                  if (valid_count <= 25) {
                      for (i=1; i<=valid_count; i++) print "  > " cleaned[i]
                  } else {
                      for (i=1; i<=15; i++) print "  > " cleaned[i]
                      print "  >"
                      print "  > ... [TRUNCATED: " (valid_count - 20) " lines omitted for readability] ..."
                      print "  >"
                      for (i=valid_count-4; i<=valid_count; i++) print "  > " cleaned[i]
                  }
                  exit
              }
              in_err=0
              buf=""
          }
      }
    ' "$CLEAN_RERUN"
    echo ""
  done
fi

echo "----------------------------------------------------------------"
echo "Summary of tests still failing:"
if [ -z "$STILL_FAILING" ]; then
  echo "None."
else
  echo "$STILL_FAILING" | sed 's/^/- /'
fi