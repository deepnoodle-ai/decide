# /tutorials/review-pull-requests/, the last step: the gate exits 2. The
# hidden step runs step 2 first, so the answers come from the cache, as
# they do for the reader.
Hide
Type "sh $REPO/demo/setup.sh shop >/dev/null && cd shop && git diff | decide run code-risk --each function >/dev/null 2>&1; clear" Enter
Wait
Show
Type "git diff | decide run code-risk --each function --fail-on flagged > /dev/null" Sleep 500ms Enter
Wait
Type "echo $?" Sleep 300ms Enter
Wait
Sleep 5s
# Expect: Exiting with code 2 because 2 items were flagged
# Expect: 8 answers from cache · 0 asked
