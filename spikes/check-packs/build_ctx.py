#!/usr/bin/env python3
"""Copy each OWASP Benchmark test case into work/bench/<category>/, and a
second copy into work/bench-ctx/<category>/ with the source of the helper
classes it calls and benchmark.properties appended."""
import glob
import os
import re
import shutil

HERE = os.path.dirname(os.path.abspath(__file__))
B = os.path.join(HERE, "work/BenchmarkJava")
TESTS = f"{B}/src/main/java/org/owasp/benchmark/testcode"
HELPERS = f"{B}/src/main/java/org/owasp/benchmark/helpers"
CALLED = {"SeparateClassRequest": ["SeparateClassRequest"],
          "ThingFactory": ["ThingFactory", "ThingInterface", "Thing1", "Thing2"]}


def strip(src):
    return re.sub(r"/\*\*.*?\*/", "", src, flags=re.S)


props = open(f"{B}/src/main/resources/benchmark.properties").read()
for line in open(f"{B}/expectedresults-1.2.csv"):
    if line.startswith("#"):
        continue
    name, cat, _, _ = line.strip().split(",")
    if cat not in ("sqli", "cmdi", "pathtraver", "xss", "crypto", "hash", "weakrand"):
        continue
    src_path = f"{TESTS}/{name}.java"
    for d in ("bench", "bench-ctx"):
        os.makedirs(os.path.join(HERE, "work", d, cat), exist_ok=True)
    shutil.copy(src_path, os.path.join(HERE, "work/bench", cat))
    src = open(src_path).read()
    extra = []
    for key, files in CALLED.items():
        if key in src:
            for h in files:
                extra.append(f"// ---- called code: {h}.java ----\n" + strip(open(f"{HELPERS}/{h}.java").read()))
    if "getProperty(" in src:
        extra.append("// ---- configuration: benchmark.properties ----\n" +
                     "\n".join("// " + l for l in props.splitlines()))
    with open(os.path.join(HERE, "work/bench-ctx", cat, name + ".java"), "w") as f:
        f.write(src + "\n\n" + "\n\n".join(extra))
print("built", len(glob.glob(os.path.join(HERE, "work/bench/*/*.java"))), "cases")
