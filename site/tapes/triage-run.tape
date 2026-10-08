# /tutorials/triage-issues/, step 2: judge the demo's issues, then read the
# three nearest their flags.
Set Height 988
Hide
Type "mkdir -p decide && cp -R $REPO/demo decide/ && cd decide/demo && clear" Enter
Wait
Show
Type "decide run task-readiness issues.json > /dev/null" Sleep 500ms Enter
Wait
Type "decide runs view --top 3" Sleep 500ms Enter
Wait
Sleep 5s
# Expect: Flagged: issues.json[1], issues.json[3], issues.json[5]
# Expect: "number":102
# Expect: "number":104
# Expect: "number":106
# Expect: The 3 of 6 answered items nearest their flags
