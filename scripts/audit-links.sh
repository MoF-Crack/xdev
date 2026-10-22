#!/bin/bash
# scripts/audit-links.sh
#
# CI release gate for download sources. Runs linkaudit in probe mode across
# the declared platforms and blocks (exit 1) when any official source is dead
# or transient. Mirror failures and known implementation gaps
# (IMPLEMENTATION_MISSING, MISSING_CHECKSUM) only warn.
#
# The authoritative gate decision is made by
#   go run ./cmd/linkaudit -ci-gate
# (internal/linkaudit/ci_gate.go, covered by ci_gate_test.go). This script
# additionally parses the JSON reports (via python3 if available) to print a
# combined cross-platform summary and list blocking entries. When python3 is
# absent, the script still enforces the gate via exit codes and relays the
# per-run BLOCK/WARN output from the CLI.
#
# Usage:
#   ./scripts/audit-links.sh                # audit all default platforms
#   ./scripts/audit-links.sh --self-test    # run script-logic tests, no network
#   ./scripts/audit-links.sh --platforms windows/amd64
#   SKIP_LINUX=1 SKIP_DARWIN=1 ./scripts/audit-links.sh
#
# Exit codes: 0 = gate passed, 1 = gate blocked or audit error, 2 = bad args.

set -u

MANIFESTS="${MANIFESTS:-./manifests}"
OUT_DIR="${OUT_DIR:-./build}"
TIMEOUT_SEC="${TIMEOUT_SEC:-30}"
CONCURRENCY="${CONCURRENCY:-4}"

# --- JSON summary helper (mirrors internal/linkaudit.GateDecisionFor) ---
# stdin: JSON array of Result objects; stdout: "total pass fail transient warn"
# plus lines "BLOCK|WARN  <tool> <version> <os>/<arch> <role> -> <status>  (<url>)"
summary_py='
import json, sys
rows = json.load(sys.stdin)
counts = {"total":0,"pass":0,"fail":0,"transient":0,"warn":0}
blocking=[]; warnings=[]
for r in rows:
    counts["total"] += 1
    st = r.get("failureReason",""); role = r.get("role","")
    if st == "PASS": counts["pass"] += 1; continue
    if st in ("IMPLEMENTATION_MISSING","MISSING_CHECKSUM") or role == "mirror":
        counts["warn"] += 1; warnings.append(r); continue
    counts["fail" if st != "TRANSIENT_ERROR" else "transient"] += 1
    blocking.append(r)
print("SUM\t%d\t%d\t%d\t%d\t%d\t%d" % (counts["total"],counts["pass"],counts["fail"],counts["transient"],counts["warn"],1 if blocking else 0))
for r in blocking:
    print("BLOCK\t%s\t%s\t%s/%s\t%s\t%s\t%s" % (r.get("tool",""),r.get("version",""),r.get("os",""),r.get("arch",""),r.get("role",""),r.get("failureReason",""),r.get("sourceUrl","")))
for r in warnings:
    print("WARN\t%s\t%s\t%s/%s\t%s\t%s\t%s" % (r.get("tool",""),r.get("version",""),r.get("os",""),r.get("arch",""),r.get("role",""),r.get("failureReason",""),r.get("sourceUrl","")))
'

have_python=0
if command -v python3 >/dev/null 2>&1; then
    PY=python3; have_python=1
elif command -v python >/dev/null 2>&1; then
    PY=python; have_python=1
fi

summarize_report() {
    # $1 = report path ; echoes TSV lines (SUM/BLOCK/WARN) or nothing
    if [ "$have_python" -eq 1 ] && [ -f "$1" ]; then
        "$PY" -c "$summary_py" < "$1"
    fi
}

# --- self-test ---
run_self_test() {
    local tmp
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    cat >"$tmp/pass.json" <<'EOF'
[
  {"tool":"a","version":"1","os":"windows","arch":"amd64","role":"official","sourceUrl":"https://ok.invalid/a.zip","failureReason":"PASS"},
  {"tool":"a","version":"1","os":"windows","arch":"amd64","role":"mirror","sourceUrl":"https://ok.invalid/m.zip","failureReason":"PASS"}
]
EOF
    cat >"$tmp/block.json" <<'EOF'
[
  {"tool":"b","version":"1","os":"windows","arch":"amd64","role":"official","sourceUrl":"https://ok.invalid/b.zip","failureReason":"PASS"},
  {"tool":"b","version":"2","os":"windows","arch":"amd64","role":"official","sourceUrl":"https://dead.invalid/b2.zip","failureReason":"URL_DEAD"}
]
EOF
    cat >"$tmp/warn.json" <<'EOF'
[
  {"tool":"c","version":"1","os":"linux","arch":"x86_64","role":"mirror","sourceUrl":"https://mir.invalid/base/","failureReason":"IMPLEMENTATION_MISSING"},
  {"tool":"c","version":"1","os":"linux","arch":"x86_64","role":"mirror","sourceUrl":"https://mir.invalid/c.zip","failureReason":"MIRROR_DEAD"},
  {"tool":"c","version":"2","os":"linux","arch":"x86_64","role":"mirror","sourceUrl":"https://mir.invalid/c2.zip","failureReason":"TRANSIENT_ERROR"}
]
EOF
    if [ "$have_python" -ne 1 ]; then
        echo "self-test requires python3/python to parse synthetic JSON" >&2
        return 1
    fi
    local failures=0
    # pass.json: exit 0, no BLOCK, no WARN
    out="$(summarize_report "$tmp/pass.json")"
    exit_code="$(printf '%s\n' "$out" | awk -F'\t' '/^SUM/ {print $7}')"
    blocks="$(printf '%s\n' "$out" | grep -c '^BLOCK' || true)"
    warns="$(printf '%s\n' "$out" | grep -c '^WARN' || true)"
    if [ "$exit_code" = "0" ] && [ "$blocks" = "0" ] && [ "$warns" = "0" ]; then
        echo "  [OK]   pass.json: exit=0 no blocks no warnings"
    else
        echo "  [FAIL] pass.json: exit=$exit_code blocks=$blocks warns=$warns" >&2
        failures=$((failures+1))
    fi
    # block.json: exit 1, exactly 1 BLOCK, 0 WARN
    out="$(summarize_report "$tmp/block.json")"
    exit_code="$(printf '%s\n' "$out" | awk -F'\t' '/^SUM/ {print $7}')"
    blocks="$(printf '%s\n' "$out" | grep -c '^BLOCK' || true)"
    warns="$(printf '%s\n' "$out" | grep -c '^WARN' || true)"
    if [ "$exit_code" = "1" ] && [ "$blocks" = "1" ] && [ "$warns" = "0" ]; then
        echo "  [OK]   block.json: exit=1 one official URL_DEAD blocked"
    else
        echo "  [FAIL] block.json: exit=$exit_code blocks=$blocks warns=$warns" >&2
        failures=$((failures+1))
    fi
    # warn.json: exit 0, 0 BLOCK, 3 WARN (incl. mirror transient)
    out="$(summarize_report "$tmp/warn.json")"
    exit_code="$(printf '%s\n' "$out" | awk -F'\t' '/^SUM/ {print $7}')"
    blocks="$(printf '%s\n' "$out" | grep -c '^BLOCK' || true)"
    warns="$(printf '%s\n' "$out" | grep -c '^WARN' || true)"
    if [ "$exit_code" = "0" ] && [ "$blocks" = "0" ] && [ "$warns" = "3" ]; then
        echo "  [OK]   warn.json: exit=0 three warnings (mirror dead/impl/transient)"
    else
        echo "  [FAIL] warn.json: exit=$exit_code blocks=$blocks warns=$warns" >&2
        failures=$((failures+1))
    fi

    if [ "$failures" -gt 0 ]; then
        echo "" >&2
        echo "SelfTest FAILED with $failures error(s)" >&2
        return 1
    fi
    echo ""
    echo "SelfTest passed (3/3)"
    return 0
}

# --- arg parsing ---
PLATFORMS_OVERRIDE=""
SKIP_WINDOWS=0; SKIP_LINUX=0; SKIP_DARWIN=0
SELF_TEST=0
while [ $# -gt 0 ]; do
    case "$1" in
        --self-test) SELF_TEST=1; shift ;;
        --platforms) PLATFORMS_OVERRIDE="$2"; shift 2 ;;
        --manifests) MANIFESTS="$2"; shift 2 ;;
        --out-dir)   OUT_DIR="$2"; shift 2 ;;
        -h|--help)
            sed -n '2,/^$/p' "$0"
            exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

if [ "$SELF_TEST" -eq 1 ]; then
    run_self_test
    exit $?
fi

# --- target list ---
suffix_for() {
    case "$1" in
        windows/*) echo "" ;;
        linux/*)   echo "-linux" ;;
        darwin/*)  echo "-darwin" ;;
        *)         echo "-p${RANDOM}" ;;
    esac
}

targets=()
if [ -n "$PLATFORMS_OVERRIDE" ]; then
    IFS=',' read -ra _pa <<<"$PLATFORMS_OVERRIDE"
    for p in "${_pa[@]}"; do
        targets+=("$p|download-audit-ci$(suffix_for "$p").json")
    done
else
    [ "${SKIP_WINDOWS:-0}" != "1" ] && targets+=("windows/amd64|download-audit-ci.json")
    [ "${SKIP_LINUX:-0}"   != "1" ] && targets+=("linux/x86_64|download-audit-ci-linux.json")
    [ "${SKIP_DARWIN:-0}"  != "1" ] && targets+=("darwin/arm64|download-audit-ci-darwin.json")
fi

if [ "${#targets[@]}" -eq 0 ]; then
    echo "no platforms selected" >&2
    exit 2
fi
if [ ! -d "$MANIFESTS" ]; then
    echo "manifest directory not found: $MANIFESTS" >&2
    exit 2
fi
mkdir -p "$OUT_DIR"

echo ""
echo "========================================"
echo "  Download source audit (probe gate)"
echo "========================================"
echo "  manifests : $MANIFESTS"
echo "  out dir   : $OUT_DIR"
plats=""
for t in "${targets[@]}"; do plats+="${t%%|*}, "; done
echo "  platforms : ${plats%, }"
echo ""

script_failed=0
c_total=0; c_pass=0; c_fail=0; c_tr=0; c_warn=0
c_blocking_lines=""

for entry in "${targets[@]}"; do
    platform="${entry%%|*}"
    report="${entry#*|}"
    report_path="$OUT_DIR/$report"
    echo "==> probe $platform"
    echo "    report -> $report_path"

    go run ./cmd/linkaudit \
        -manifests "$MANIFESTS" \
        -mode probe \
        -platform "$platform" \
        -timeout "${TIMEOUT_SEC}s" \
        -concurrency "$CONCURRENCY" \
        -report "$report_path" \
        -ci-gate 2>&1 | sed 's/^/    /'
    run_exit=${PIPESTATUS[0]}

    if [ "$run_exit" -ne 0 ] && [ "$run_exit" -ne 1 ]; then
        echo "    [ERROR] linkaudit exited with code $run_exit for $platform" >&2
        script_failed=1
        continue
    fi
    if [ ! -f "$report_path" ]; then
        echo "    [ERROR] report not produced: $report_path" >&2
        script_failed=1
        continue
    fi

    # Parse combined counts (best-effort); exit code is authoritative.
    if [ "$have_python" -eq 1 ]; then
        sum_out="$(summarize_report "$report_path")"
        sum_line="$(printf '%s\n' "$sum_out" | grep '^SUM' || true)"
        if [ -n "$sum_line" ]; then
            t=$(printf '%s' "$sum_line" | awk -F'\t' '{print $2}')
            p=$(printf '%s' "$sum_line" | awk -F'\t' '{print $3}')
            f=$(printf '%s' "$sum_line" | awk -F'\t' '{print $4}')
            tr=$(printf '%s' "$sum_line" | awk -F'\t' '{print $5}')
            w=$(printf '%s' "$sum_line" | awk -F'\t' '{print $6}')
            ex=$(printf '%s' "$sum_line" | awk -F'\t' '{print $7}')
            printf '    %-14s total=%s pass=%s fail=%s transient=%s warn=%s exit=%s\n' "$platform" "$t" "$p" "$f" "$tr" "$w" "$ex"
            c_total=$((c_total+t)); c_pass=$((c_pass+p)); c_fail=$((c_fail+f))
            c_tr=$((c_tr+tr)); c_warn=$((c_warn+w))
        fi
        # relay blocking + warning rows to stderr
        printf '%s\n' "$sum_out" | grep '^BLOCK' | sed 's/^/  /' >&2 || true
        printf '%s\n' "$sum_out" | grep '^WARN'  | sed 's/^/  /' >&2 || true
    fi

    if [ "$run_exit" -ne 0 ]; then
        script_failed=1
    fi
done

echo ""
echo "========================================"
if [ "$have_python" -eq 1 ]; then
    echo "  Combined: total=$c_total pass=$c_pass fail=$c_fail transient=$c_tr warn=$c_warn"
fi
echo "========================================"
echo ""

if [ "$script_failed" -ne 0 ]; then
    echo "Audit gate BLOCKED the build." >&2
    exit 1
fi
echo "Audit gate passed."
exit 0
