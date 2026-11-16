#!/usr/bin/env python3
"""Show a make-up service log (.run/<service>.log) one entry per line.

The services log with frontlog (zap): pretty-printed JSON objects, mixed
with plain lines (Go's log package, build output). This prints each JSON
entry as `time LEVEL caller message key=value …` and plain lines as they
are, so the log can be read and filtered without jq choking on them.

  python3 logview.py noc_automation                  # last 100 entries
  python3 logview.py rtutil -f                       # follow (like tail -f)
  python3 logview.py noc_automation --run 008057e3   # one run (any field)
  python3 logview.py noc_automation --sql only       # rtdatacore SQL only
  python3 logview.py noc_automation --sql hide       # without the SQL
  python3 logview.py noc_automation --level warn     # WARN and above
"""
import argparse
import json
import os
import sys
import time

LEVELS = {"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3, "DPANIC": 4, "PANIC": 4, "FATAL": 5}
COLOR = {"DEBUG": "\033[2m", "INFO": "", "WARN": "\033[33m", "ERROR": "\033[31m"}
BASE = ("timestamp", "severity", "caller", "message")
# rtdatacore's GORM logger: the SQL the repositories run.
SQL_CALLER = "rtdb/gorm_logger.go"


def entries(text):
    """Yield (obj, None) for each JSON entry and (None, line) for plain lines.
    Returns the unparsed tail (an entry still being written)."""
    dec = json.JSONDecoder()
    i, n = 0, len(text)
    while i < n:
        while i < n and text[i] in " \t\r\n":
            i += 1
        if i >= n:
            break
        if text[i] == "{":
            try:
                obj, end = dec.raw_decode(text, i)
            except json.JSONDecodeError:
                return text[i:]  # incomplete: wait for more
            yield obj, None
            i = end
            continue
        j = text.find("\n", i)
        if j < 0:
            return text[i:]
        yield None, text[i:j]
        i = j + 1
    return ""


def short(v, width):
    s = v if isinstance(v, str) else json.dumps(v, ensure_ascii=False, separators=(",", ":"))
    s = " ".join(s.split())  # one entry, one line
    return s if width <= 0 or len(s) <= width else s[: width - 1] + "…"


def fmt(obj, args, color):
    level = str(obj.get("severity", "")).upper()
    t = str(obj.get("timestamp", ""))[11:23]
    extra = " ".join(f"{k}={short(v, args.width)}" for k, v in obj.items() if k not in BASE)
    line = f"{t} {level:5} {obj.get('caller', '')} {obj.get('message', '')}" + (f"  {extra}" if extra else "")
    if color and COLOR.get(level):
        line = COLOR[level] + line + "\033[0m"
    return line


def keep(obj, plain, args):
    if obj is None:
        return not args.run and args.sql != "only" and args.level is None
    is_sql = obj.get("caller", "").startswith(SQL_CALLER)
    if args.sql == "only" and not is_sql or args.sql == "hide" and is_sql:
        return False
    if args.level and LEVELS.get(str(obj.get("severity", "")).upper(), 1) < LEVELS[args.level.upper()]:
        return False
    if args.run and args.run not in json.dumps(obj, ensure_ascii=False):
        return False
    return True


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("service", help="noc_automation | rtutil | criteria_ui | pipeline_report (or a log file path)")
    ap.add_argument("-f", "--follow", action="store_true", help="keep printing new entries")
    ap.add_argument("-n", "--lines", type=int, default=100, help="entries to show first (default 100, 0 = all)")
    ap.add_argument("--run", help="only entries containing this text (run_id, ticket id …)")
    ap.add_argument("--sql", choices=["show", "only", "hide"], default="show", help="rtdatacore SQL entries")
    ap.add_argument("--level", choices=["debug", "info", "warn", "error"], help="minimum level")
    ap.add_argument("--width", type=int, default=160, help="max width of each field value (0 = full)")
    ap.add_argument("--run-dir", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), ".run"))
    args = ap.parse_args()

    path = args.service if os.path.sep in args.service else os.path.join(args.run_dir, args.service + ".log")
    if not os.path.exists(path):
        sys.exit(f"{path} not found — is {args.service} started by make up?")
    color = sys.stdout.isatty()

    with open(path, encoding="utf-8", errors="replace") as f:
        rest = f.read()
        gen = entries(rest)
        shown = []
        try:
            while True:
                obj, plain = next(gen)
                if keep(obj, plain, args):
                    shown.append(fmt(obj, args, color) if obj is not None else plain)
        except StopIteration as stop:
            rest = stop.value or ""
        for line in shown[-args.lines:] if args.lines > 0 else shown:
            print(line)
        if not args.follow:
            return
        sys.stdout.flush()
        while True:
            chunk = f.read()
            if not chunk:
                if os.path.getsize(path) < f.tell():  # truncated / restarted
                    f.seek(0)
                time.sleep(0.5)
                continue
            gen = entries(rest + chunk)
            try:
                while True:
                    obj, plain = next(gen)
                    if keep(obj, plain, args):
                        print(fmt(obj, args, color) if obj is not None else plain, flush=True)
            except StopIteration as stop:
                rest = stop.value or ""


if __name__ == "__main__":
    try:
        main()
    except (KeyboardInterrupt, BrokenPipeError):
        pass
