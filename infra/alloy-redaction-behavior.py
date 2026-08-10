#!/usr/bin/env python3
"""Behavior check for loki.process "redact_pii" in alloy/config.alloy (C11).

Reads the stage.replace expressions on stdin, in file order, exactly as they are
written in the config, and runs each against sample log lines.

WHY THIS IS NOT grep -E
Alloy compiles these with RE2, where `(?i)` and `(?:...)` are ordinary syntax.
POSIX ERE has neither, and grep does not error on them — it quietly matches
something else, so a grep-based check would have reported green for an expression
that never fires. Python's re agrees with RE2 on every construct these
expressions use.

WHY THE SUBSTITUTION IS HAND-ROLLED
stage.replace does not replace the whole match: it replaces each CAPTURE GROUP
with the `replace` value and leaves the rest of the match in place. That is the
property the expressions are designed around (it is what keeps the surrounding
JSON quoting intact), so the emulation here has to do the same thing rather than
call re.sub.

Every address below is in an RFC 2606 example domain, so nothing this script
prints on failure can be a real person's address.
"""

import json
import re
import sys

# The marker each stage writes, by position. Kept here rather than parsed so a
# swapped marker in the config shows up as a behavior failure.
DSN, CREDENTIALS, EMAIL = 0, 1, 2

# The line C11 actually found in production Loki, with the address replaced.
LEAKED = (
    '{"level":"info","caller":"email/sender.go:160","msg":"SES email sent",'
    '"service_name":"openmentor-worker","template":"new-review",'
    '"recipient":"mentee.person@example.com","message_id":"0100-abc",'
    '"duration":0.116882211}'
)
# The same line once the Go layer has done its job. Nothing may touch this.
MASKED = (
    '{"level":"info","msg":"SES email sent","template":"new-review",'
    '"recipient":"m***@example.com","message_id":"0100-abc"}'
)
# An ordinary request line: the regression case for over-redaction.
KEEP = (
    '{"level":"info","msg":"HTTP request","method":"GET","path":"/api/v1/mentors",'
    '"status":200,"duration":0.031,"trace_id":"0af7651916cd43dd8448eb211c80319c",'
    '"slug":"alice-mentor"}'
)
DSN_LEAK = (
    '{"level":"error","msg":"Failed to run migrations",'
    '"error":"cannot connect to postgres://om:sup3rs3cret@db.internal:5432/openmentor"}'
)
TOKEN_LEAK = '{"level":"warn","msg":"rejected","login_token":"abc123def456","status":401}'
# A query sample from database_observability, the path that reaches Loki without
# passing through any Go code at all.
QUERY_SAMPLE = (
    '{"level":"info","msg":"query sample",'
    '"sql":"SELECT id FROM mentors WHERE email = \'mentee.person@example.com\'"}'
)

# (expression index, label, line, must-not-survive, must-survive)
CASES = [
    (DSN, "dsn password removed from an error message", DSN_LEAK, ["sup3rs3cret"],
     ["postgres://om:", "db.internal:5432/openmentor"]),
    (DSN, "dsn rule leaves an ordinary request line alone", KEEP, [], [KEEP]),
    (DSN, "dsn rule leaves a masked recipient alone", MASKED, [], [MASKED]),

    (CREDENTIALS, "token field value removed", TOKEN_LEAK, ["abc123def456"],
     ['"login_token"', '"status":401']),
    (CREDENTIALS, "credential rule leaves an ordinary request line alone", KEEP, [], [KEEP]),
    # The regression that made the key list explicit instead of a substring rule.
    (CREDENTIALS, "credential rule leaves login_token_ttl_minutes alone",
     '{"msg":"login requested","login_token_ttl_minutes":15}', [],
     ['"login_token_ttl_minutes":15']),

    (EMAIL, "the address C11 found in Loki is removed", LEAKED,
     ["mentee.person", "mentee.person@example.com"],
     ['"template":"new-review"', '"message_id":"0100-abc"', "@example.com"]),
    (EMAIL, "an address inside a db-observability query sample is removed",
     QUERY_SAMPLE, ["mentee.person"], ["SELECT id FROM mentors"]),
    (EMAIL, "an already-masked address is not mangled twice", MASKED, [], [MASKED]),
    (EMAIL, "email rule leaves an ordinary request line alone", KEEP, [], [KEEP]),
    (EMAIL, "a module path with an @version is not an address",
     '{"msg":"build failed for github.com/openmentor-io/openmentor@v1.2.3"}', [],
     ["openmentor@v1.2.3"]),
    (EMAIL, "a digest-pinned image is not an address",
     '{"msg":"pulling grafana/alloy@sha256:deadbeefdeadbeef"}', [],
     ["alloy@sha256:deadbeefdeadbeef"]),
]

MARKERS = {DSN: "[REDACTED_SECRET]", CREDENTIALS: "[REDACTED_SECRET]", EMAIL: "[REDACTED_EMAIL]"}


def unescape_river(raw: str) -> str:
    """Turn a river double-quoted string body into the bytes the engine gets."""
    return raw.encode("utf-8").decode("unicode_escape")


def alloy_replace(pattern: "re.Pattern[str]", replacement: str, line: str) -> str:
    """Emulate stage.replace: substitute every capture group, keep the rest."""
    spans = []
    for match in pattern.finditer(line):
        if match.re.groups == 0:
            spans.append(match.span())
            continue
        for group in range(1, match.re.groups + 1):
            if match.span(group) != (-1, -1):
                spans.append(match.span(group))

    out, cursor = [], 0
    for start, end in sorted(spans):
        if start < cursor:  # overlapping groups: first one wins
            continue
        out.append(line[cursor:start])
        out.append(replacement)
        cursor = end
    out.append(line[cursor:])
    return "".join(out)


def main() -> int:
    raw = [line.rstrip("\n") for line in sys.stdin if line.strip()]
    if len(raw) < 3:
        print(f"    expected at least 3 expressions on stdin, got {len(raw)}")
        return 1

    patterns = []
    for index, body in enumerate(raw):
        try:
            patterns.append(re.compile(unescape_river(body)))
        except re.error as err:
            print(f"    expression {index + 1} does not compile: {err}\n      {body}")
            return 1

    failures = 0
    for index, label, line, forbidden, required in CASES:
        result = alloy_replace(patterns[index], MARKERS[index], line)

        problems = [f"still carries {value!r}" for value in forbidden if value in result]
        problems += [f"lost {value!r}" for value in required if value not in result]

        # Case 5: the query-time JSON parser and every dashboard depend on this.
        try:
            json.loads(result)
        except json.JSONDecodeError as err:
            problems.append(f"is no longer valid JSON ({err})")

        if problems:
            failures += 1
            print(f"    FAIL {label}")
            for problem in problems:
                print(f"         {problem}")
            print(f"         got: {result}")

    if failures:
        print(f"    {failures} behavior case(s) failed")
        return 1
    print(f"    {len(CASES)} behavior cases passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
