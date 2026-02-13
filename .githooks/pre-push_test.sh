#!/bin/bash
# Tests for the pre-push hook integration branch safety guardrail.
# Run from repo root: bash .githooks/pre-push_test.sh

HOOK="$(cd "$(dirname "$0")" && pwd)/pre-push"
PASS=0
FAIL=0

assert_exit() {
  local expected_exit="$1"
  local desc="$2"
  local actual_exit="$3"
  if [[ "$actual_exit" -eq "$expected_exit" ]]; then
    PASS=$((PASS + 1))
    echo "  PASS: $desc"
  else
    FAIL=$((FAIL + 1))
    if [[ "$expected_exit" -eq 0 ]]; then
      echo "  FAIL: $desc (expected allowed, got exit $actual_exit)"
    else
      echo "  FAIL: $desc (expected blocked, got exit $actual_exit)"
    fi
  fi
}

# Each test runs in a subshell to isolate cd and state
run_test() {
  local test_name="$1"
  local test_fn="$2"
  echo "$test_name"
  eval "$test_fn"
}

ZERO_SHA="0000000000000000000000000000000000000000"

# ---------- Test 1: Normal push to main ----------
test1() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    echo change >> f && git add f && git commit -m change >/dev/null 2>&1
    local lsha=$(git rev-parse HEAD)
    local rsha=$(git rev-parse HEAD~1)
    printf "refs/heads/main %s refs/heads/main %s\n" "$lsha" "$rsha" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test1)
assert_exit 0 "normal push to main" "$result"

# ---------- Test 2: Push to polecat/* ----------
test2() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    git checkout -b polecat/work >/dev/null 2>&1
    echo work >> f && git add f && git commit -m work >/dev/null 2>&1
    local lsha=$(git rev-parse HEAD)
    printf "refs/heads/polecat/work %s refs/heads/polecat/work %s\n" "$lsha" "$ZERO_SHA" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test2)
assert_exit 0 "push to polecat/*" "$result"

# ---------- Test 3: Push to integration/* (new — allowed) ----------
test3() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    git checkout -b integration/epic >/dev/null 2>&1
    echo work >> f && git add f && git commit -m work >/dev/null 2>&1
    local lsha=$(git rev-parse HEAD)
    printf "refs/heads/integration/epic %s refs/heads/integration/epic %s\n" "$lsha" "$ZERO_SHA" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test3)
assert_exit 0 "push to integration/*" "$result"

# ---------- Test 4: Push to random branch without upstream (blocked) ----------
test4() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    local lsha=$(git rev-parse HEAD)
    printf "refs/heads/feature/random %s refs/heads/feature/random %s\n" "$lsha" "$ZERO_SHA" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test4)
assert_exit 1 "push to feature/* without upstream remote" "$result"

# ---------- Test 5: Push to random branch with upstream (allowed) ----------
test5() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks
    git remote add upstream https://example.com/upstream.git

    local lsha=$(git rev-parse HEAD)
    printf "refs/heads/feature/random %s refs/heads/feature/random %s\n" "$lsha" "$ZERO_SHA" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test5)
assert_exit 0 "push to feature/* with upstream remote" "$result"

# ---------- Test 6: Push to main with integration merge — BLOCKED ----------
test6() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    # Create integration branch and push to origin
    git checkout -b integration/epic >/dev/null 2>&1
    echo epic >> f && git add f && git commit -m "epic work" >/dev/null 2>&1
    git push origin integration/epic >/dev/null 2>&1

    # Back to main — add a diverging commit so --no-ff creates a real merge
    git checkout main >/dev/null 2>&1
    echo "other" > g && git add g && git commit -m "other work on main" >/dev/null 2>&1
    git push origin main >/dev/null 2>&1

    # Merge integration branch into main
    git fetch origin >/dev/null 2>&1
    local rsha=$(git rev-parse HEAD)
    git merge --no-ff origin/integration/epic -m "land epic" >/dev/null 2>&1
    local lsha=$(git rev-parse HEAD)

    printf "refs/heads/main %s refs/heads/main %s\n" "$lsha" "$rsha" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test6)
assert_exit 1 "push to main with integration merge (no env var)" "$result"

# ---------- Test 7: Push to main with integration merge + env var — ALLOWED ----------
test7() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    # Create integration branch and push to origin
    git checkout -b integration/epic >/dev/null 2>&1
    echo epic >> f && git add f && git commit -m "epic work" >/dev/null 2>&1
    git push origin integration/epic >/dev/null 2>&1

    # Back to main — add a diverging commit so --no-ff creates a real merge
    git checkout main >/dev/null 2>&1
    echo "other" > g && git add g && git commit -m "other work on main" >/dev/null 2>&1
    git push origin main >/dev/null 2>&1

    # Merge integration branch into main
    git fetch origin >/dev/null 2>&1
    local rsha=$(git rev-parse HEAD)
    git merge --no-ff origin/integration/epic -m "land epic" >/dev/null 2>&1
    local lsha=$(git rev-parse HEAD)

    printf "refs/heads/main %s refs/heads/main %s\n" "$lsha" "$rsha" | GT_INTEGRATION_LAND=1 bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test7)
assert_exit 0 "push to main with integration merge (GT_INTEGRATION_LAND=1)" "$result"

# ---------- Test 8: Push to main with non-integration merge (allowed) ----------
test8() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    # Create a regular branch (not integration/*) and push
    git checkout -b bugfix/stuff >/dev/null 2>&1
    echo fix >> f && git add f && git commit -m "bugfix" >/dev/null 2>&1
    git push origin bugfix/stuff >/dev/null 2>&1

    # Merge into main (non-integration merge)
    git checkout main >/dev/null 2>&1
    local rsha=$(git rev-parse HEAD)
    git merge --no-ff origin/bugfix/stuff -m "merge bugfix" >/dev/null 2>&1
    local lsha=$(git rev-parse HEAD)

    printf "refs/heads/main %s refs/heads/main %s\n" "$lsha" "$rsha" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test8)
assert_exit 0 "push to main with non-integration merge" "$result"

# ---------- Test 9: Tag push (always allowed) ----------
test9() {
  local d=$(mktemp -d)
  (
    git init --bare "$d/remote.git" >/dev/null 2>&1
    git clone "$d/remote.git" "$d/local" >/dev/null 2>&1
    cd "$d/local"
    git config user.email "t@t" && git config user.name "T"
    echo init > f && git add f && git commit -m init >/dev/null 2>&1
    git branch -M main >/dev/null 2>&1
    git push origin main >/dev/null 2>&1
    mkdir -p .githooks && cp "$HOOK" .githooks/pre-push && chmod +x .githooks/pre-push
    git config core.hooksPath .githooks

    local lsha=$(git rev-parse HEAD)
    printf "refs/tags/v1.0 %s refs/tags/v1.0 %s\n" "$lsha" "$ZERO_SHA" | bash .githooks/pre-push >/dev/null 2>&1
    echo $?
  )
  rm -rf "$d"
}
result=$(test9)
assert_exit 0 "tag push" "$result"

# ---------- Summary ----------
echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
