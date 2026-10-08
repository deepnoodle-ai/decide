# /tutorials/review-pull-requests/, step 2: each changed function in the
# demo's shop repository.
Set Height 988
Hide
Type "sh $REPO/demo/setup.sh shop >/dev/null && cd shop && clear" Enter
Wait
Show
Type "git diff | decide run code-risk --each function" Sleep 500ms Enter
Wait
Sleep 5s
# Expect: billing/refund.go#L16  Refunds.Apply
# Expect: billing/refund.go#L23  Refunds.Search
# Expect: ! 2 flagged
