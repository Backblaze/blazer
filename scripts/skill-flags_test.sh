#!/usr/bin/env bash
# Check that the b2keys skill only names flags defined by the tool.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
python3 - "$root" <<'PY'
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
skill = (root / '.claude/skills/b2keys/SKILL.md').read_text()
source = (root / 'bin/b2keys/b2keys.go').read_text()
flags = set(re.findall(r'(?<![\w-])--?([a-z][a-z0-9-]*)', skill))
defined = set(re.findall(r'fs\.\w+\("([a-z][a-z0-9-]*)"', source))
missing = sorted(flags - defined)
if missing:
    print('FAIL: flags not defined by b2keys: ' + ', '.join(missing))
    sys.exit(1)
print('ok   b2keys skill flags are defined by the tool')
PY
