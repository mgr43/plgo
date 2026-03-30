#!/bin/bash
set -e

export PATH="/usr/lib/postgresql/18/bin:$PATH"
export PGDATA=/var/lib/postgresql/data

# Initialize and start PostgreSQL
mkdir -p "$PGDATA"
chown postgres:postgres "$PGDATA"
gosu postgres initdb -D "$PGDATA" > /dev/null 2>&1
gosu postgres pg_ctl -D "$PGDATA" -l /tmp/pg.log start > /dev/null 2>&1
sleep 2

PASS=0
FAIL=0
ERRORS=""

# normalize: strip timestamps from NOTICE/ERROR lines (HH:MM:SS pattern)
normalize() {
    sed -E 's/[0-9]{2}:[0-9]{2}:[0-9]{2}/HH:MM:SS/g'
}

for sqlfile in /test/sql/*.sql; do
    name=$(basename "$sqlfile" .sql)
    expected="/test/expected/${name}.out"
    actual="/tmp/${name}.actual"

    # Run the SQL file, capture output, normalize timestamps
    gosu postgres psql -X -a -f "$sqlfile" postgres 2>&1 | normalize > "$actual" || true

    if [ ! -f "$expected" ]; then
        echo "SKIP $name (no expected output file)"
        continue
    fi

    if diff -u "$expected" "$actual" > "/tmp/${name}.diff" 2>&1; then
        echo "PASS $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL $name"
        cat "/tmp/${name}.diff"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS $name"
    fi
done

echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
if [ $FAIL -gt 0 ]; then
    echo "Failed tests:$ERRORS"
    exit 1
fi
echo "All tests passed!"
