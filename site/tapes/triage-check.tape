# /tutorials/triage-issues/, the last step: the workflow's command on a
# vague issue exits 2. The pipe ends a line, as in the workflow.
Type "TITLE='Checkout is slow'; BODY='Customers say checkout feels slow.'" Enter
Wait
Type `printf '%s\n\n%s\n' "$TITLE" "$BODY" |` Enter
Type "  decide run task-readiness --each file --fail-on flagged" Sleep 500ms Enter
Wait
Type "echo $?" Sleep 300ms Enter
Wait
Sleep 5s
# Expect: ! ready  no
# Expect: Exiting with code 2
